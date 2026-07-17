package shared

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const TranscriptContentType = "application/json"

func WriteTranscriptFile(path string, document TranscriptDocument) (string, int64, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("create transcript file: %w", err)
	}
	hash := sha256.New()
	compressed := gzip.NewWriter(file)
	encodeErr := json.NewEncoder(io.MultiWriter(compressed, hash)).Encode(document)
	gzipErr := compressed.Close()
	closeErr := file.Close()
	if encodeErr != nil {
		return "", 0, fmt.Errorf("encode transcript: %w", encodeErr)
	}
	if gzipErr != nil {
		return "", 0, fmt.Errorf("compress transcript: %w", gzipErr)
	}
	if closeErr != nil {
		return "", 0, fmt.Errorf("close transcript file: %w", closeErr)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, fmt.Errorf("stat transcript file: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), info.Size(), nil
}

func ReadTranscript(reader io.Reader) (TranscriptDocument, error) {
	compressed, err := gzip.NewReader(reader)
	if err != nil {
		return TranscriptDocument{}, fmt.Errorf("open transcript gzip: %w", err)
	}
	defer compressed.Close()
	var document TranscriptDocument
	if err := json.NewDecoder(compressed).Decode(&document); err != nil {
		return TranscriptDocument{}, fmt.Errorf("decode transcript: %w", err)
	}
	if document.Version != TranscriptVersion {
		return TranscriptDocument{}, fmt.Errorf("unsupported transcript version %d", document.Version)
	}
	return document, nil
}

func UploadTranscript(
	ctx context.Context,
	storage *Storage,
	key, path, checksum string,
	size int64,
) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open transcript upload: %w", err)
	}
	defer file.Close()
	_, err = storage.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(storage.Bucket), Key: aws.String(key), Body: file,
		ContentLength: aws.Int64(size), ContentType: aws.String(TranscriptContentType),
		ContentEncoding: aws.String("gzip"),
		Metadata:        map[string]string{"sha256": checksum},
	})
	if err != nil {
		return fmt.Errorf("put transcript: %w", err)
	}
	return nil
}

func DownloadTranscript(
	ctx context.Context,
	storage *Storage,
	key string,
) (TranscriptDocument, error) {
	object, err := storage.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(storage.Bucket), Key: aws.String(key),
	})
	if err != nil {
		return TranscriptDocument{}, fmt.Errorf("get transcript: %w", err)
	}
	defer object.Body.Close()
	return ReadTranscript(object.Body)
}
