package shared

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/rs/zerolog"
)

func TestPresignedURLUsesTheBrowserVisibleEndpoint(t *testing.T) {
	cfg := Config{S3: S3Config{
		Endpoint:        "http://minio:9000",
		PublicEndpoint:  "http://localhost:9000",
		Region:          "auto",
		Bucket:          "hookary-media-test",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
		ForcePathStyle:  true,
		PresignTTL:      15 * time.Minute,
	}}
	storage, err := NewStorage(context.Background(), cfg, zerolog.Nop())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	signed, err := storage.Presign.PresignPutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(storage.Bucket), Key: aws.String("source/test.mp4"),
	})
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}
	parsed, err := url.Parse(signed.URL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	if parsed.Host != "localhost:9000" {
		t.Fatalf("presigned host = %q, want browser-visible localhost:9000", parsed.Host)
	}
}
