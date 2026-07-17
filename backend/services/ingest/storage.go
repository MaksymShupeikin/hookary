package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

var errMediaTooLarge = errors.New("media exceeds the ingest size limit")

func downloadObject(
	ctx context.Context,
	storage *shared.Storage,
	key, destination string,
	expectedSize *int64,
	maxBytes int64,
) (int64, string, error) {
	object, err := storage.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(storage.Bucket), Key: aws.String(key),
	})
	if err != nil {
		return 0, "", fmt.Errorf("get source object: %w", err)
	}
	defer object.Body.Close()
	if object.ContentLength != nil && *object.ContentLength > maxBytes {
		return 0, "", errMediaTooLarge
	}

	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, "", fmt.Errorf("create local source: %w", err)
	}
	hash := sha256.New()
	limited := &io.LimitedReader{R: object.Body, N: maxBytes + 1}
	written, copyErr := io.Copy(io.MultiWriter(file, hash), limited)
	closeErr := file.Close()
	if copyErr != nil {
		return 0, "", fmt.Errorf("download source object: %w", copyErr)
	}
	if closeErr != nil {
		return 0, "", fmt.Errorf("close local source: %w", closeErr)
	}
	if written > maxBytes {
		return 0, "", errMediaTooLarge
	}
	if object.ContentLength != nil && written != *object.ContentLength {
		return 0, "", errors.New("source object ended before its declared length")
	}
	if expectedSize != nil && written != *expectedSize {
		return 0, "", errors.New("source object size changed after upload")
	}
	return written, hex.EncodeToString(hash.Sum(nil)), nil
}

func uploadObject(
	ctx context.Context,
	storage *shared.Storage,
	key, source, contentType, checksum string,
) (int64, error) {
	file, err := os.Open(source)
	if err != nil {
		return 0, fmt.Errorf("open normalized source: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat normalized source: %w", err)
	}
	_, err = storage.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(storage.Bucket), Key: aws.String(key),
		Body: file, ContentLength: aws.Int64(info.Size()), ContentType: aws.String(contentType),
		Metadata: map[string]string{"sha256": checksum},
	})
	if err != nil {
		return 0, fmt.Errorf("put normalized source: %w", err)
	}
	return info.Size(), nil
}

func hashFile(path string, maxBytes int64) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer file.Close()
	hash := sha256.New()
	limited := &io.LimitedReader{R: file, N: maxBytes + 1}
	size, err := io.Copy(hash, limited)
	if err != nil {
		return 0, "", err
	}
	if size > maxBytes {
		return 0, "", errMediaTooLarge
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}
