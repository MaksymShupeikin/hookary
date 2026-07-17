package shared

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/rs/zerolog"
)

// Storage wraps the S3 client and the bucket it is bound to.
//
// The same type talks to Cloudflare R2 in production and MinIO locally; only
// config differs. Credentials live here and only here — they are never handed
// to a client.
type Storage struct {
	Client  *s3.Client
	Presign *s3.PresignClient
	Bucket  string

	presignTTL time.Duration
}

// NewStorage builds the S3 client.
//
// Credentials are static and come from config. The default AWS chain is
// deliberately not used: it would silently fall back to an instance role or
// ~/.aws/credentials and talk to the wrong account rather than fail.
func NewStorage(ctx context.Context, cfg Config, logger zerolog.Logger) (*Storage, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey, "",
			),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	clientOptions := func(endpoint string) func(*s3.Options) {
		return func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = cfg.S3.ForcePathStyle
		}
	}
	client := s3.NewFromConfig(awsCfg, clientOptions(cfg.S3.Endpoint))
	// Containers use the internal endpoint for server-side calls, while a
	// browser needs a host it can actually resolve. The signature includes the
	// host, so presigning must use the browser-visible endpoint from the start.
	presignClient := s3.NewFromConfig(awsCfg, clientOptions(cfg.S3.PublicEndpoint))

	logger.Info().
		Str("endpoint", cfg.S3.Endpoint).
		Str("public_endpoint", cfg.S3.PublicEndpoint).
		Str("bucket", cfg.S3.Bucket).
		Bool("path_style", cfg.S3.ForcePathStyle).
		Msg("storage configured")

	return &Storage{
		Client:     client,
		Presign:    s3.NewPresignClient(presignClient),
		Bucket:     cfg.S3.Bucket,
		presignTTL: cfg.S3.PresignTTL,
	}, nil
}

// PresignTTL is how long a minted URL stays valid.
func (s *Storage) PresignTTL() time.Duration { return s.presignTTL }

// Probe reports storage health for /health/ready.
//
// It heads the bucket rather than listing it: the answer needed is "can we
// reach the bucket and are the credentials good", and HeadBucket says that
// without paging through objects.
func (s *Storage) Probe() ReadyFunc {
	return func(ctx context.Context) error {
		_, err := s.Client.HeadBucket(ctx, &s3.HeadBucketInput{
			Bucket: aws.String(s.Bucket),
		})
		if err != nil {
			return fmt.Errorf("storage: %w", err)
		}
		return nil
	}
}
