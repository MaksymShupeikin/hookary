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

const maximumTranscriptionFileBytes = int64(24 << 20)

func downloadAudioChunk(
	ctx context.Context,
	storage *shared.Storage,
	chunk audioChunk,
	destination string,
) error {
	if chunk.SizeBytes <= 0 || chunk.SizeBytes > maximumTranscriptionFileBytes {
		return errors.New("audio chunk size is outside the transcription limit")
	}
	object, err := storage.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(storage.Bucket), Key: aws.String(chunk.ObjectKey),
	})
	if err != nil {
		return fmt.Errorf("get audio chunk: %w", err)
	}
	defer object.Body.Close()
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create audio chunk: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(object.Body, maximumTranscriptionFileBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("download audio chunk: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close audio chunk: %w", closeErr)
	}
	if written != chunk.SizeBytes || written > maximumTranscriptionFileBytes {
		return errors.New("audio chunk changed after ingest")
	}
	return nil
}
