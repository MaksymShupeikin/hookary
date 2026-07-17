package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

func downloadRenderSource(
	ctx context.Context,
	storage *shared.Storage,
	key, destination string,
	expectedSize, maximumSize int64,
) error {
	if expectedSize <= 0 || expectedSize > maximumSize {
		return errors.New("source size is outside the render limit")
	}
	object, err := storage.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(storage.Bucket), Key: aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("get render source: %w", err)
	}
	defer object.Body.Close()
	if object.ContentLength == nil || *object.ContentLength != expectedSize {
		return errors.New("source object size changed before render")
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create render source: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(object.Body, maximumSize+1))
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("download render source: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close render source: %w", closeErr)
	}
	if written != expectedSize {
		return errors.New("source object ended before its declared length")
	}
	return nil
}

func uploadRenderObject(
	ctx context.Context,
	storage *shared.Storage,
	key, source, contentType string,
) error {
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open render output: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat render output: %w", err)
	}
	if info.Size() <= 0 {
		return errors.New("render output is empty")
	}
	_, err = storage.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(storage.Bucket), Key: aws.String(key), Body: file,
		ContentLength: aws.Int64(info.Size()), ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put render output: %w", err)
	}
	return nil
}
