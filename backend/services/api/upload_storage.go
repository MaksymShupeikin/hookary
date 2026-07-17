package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type objectMetadata struct {
	SizeBytes   int64
	ETag        string
	ContentType string
}

func (s *server) presignSingleUpload(
	ctx context.Context,
	key, contentType string,
) (string, time.Time, error) {
	result, err := s.storage.Presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.storage.Bucket),
		Key:    aws.String(key), ContentType: aws.String(contentType),
	}, s.presignOptions)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign put object: %w", err)
	}
	return result.URL, time.Now().Add(s.storage.PresignTTL()), nil
}

func (s *server) createMultipartUpload(
	ctx context.Context,
	key, contentType string,
) (string, error) {
	result, err := s.storage.Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.storage.Bucket),
		Key:    aws.String(key), ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("create multipart upload: %w", err)
	}
	if result.UploadId == nil || *result.UploadId == "" {
		return "", errors.New("create multipart upload returned no upload id")
	}
	return *result.UploadId, nil
}

func (s *server) presignUploadPart(
	ctx context.Context,
	key, uploadID string,
	partNumber int32,
) (string, time.Time, error) {
	result, err := s.storage.Presign.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(s.storage.Bucket), Key: aws.String(key),
		UploadId: aws.String(uploadID), PartNumber: aws.Int32(partNumber),
	}, s.presignOptions)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign upload part %d: %w", partNumber, err)
	}
	return result.URL, time.Now().Add(s.storage.PresignTTL()), nil
}

func (s *server) listMultipartParts(
	ctx context.Context,
	key, uploadID string,
) ([]uploadedPartResponse, error) {
	pager := s3.NewListPartsPaginator(s.storage.Client, &s3.ListPartsInput{
		Bucket: aws.String(s.storage.Bucket), Key: aws.String(key),
		UploadId: aws.String(uploadID),
	})
	parts := make([]uploadedPartResponse, 0)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, part := range page.Parts {
			if part.PartNumber == nil || part.ETag == nil || part.Size == nil {
				return nil, errors.New("storage returned an incomplete multipart part")
			}
			parts = append(parts, uploadedPartResponse{
				PartNumber: *part.PartNumber,
				ETag:       *part.ETag,
				SizeBytes:  *part.Size,
			})
		}
	}
	return parts, nil
}

func (s *server) finishMultipartUpload(
	ctx context.Context,
	key, uploadID string,
	parts []uploadedPartResponse,
) error {
	completed := make([]types.CompletedPart, 0, len(parts))
	for _, part := range parts {
		completed = append(completed, types.CompletedPart{
			ETag: aws.String(part.ETag), PartNumber: aws.Int32(part.PartNumber),
		})
	}
	_, err := s.storage.Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(s.storage.Bucket), Key: aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	})
	if err != nil {
		return fmt.Errorf("complete multipart upload: %w", err)
	}
	return nil
}

func (s *server) abortMultipartUpload(ctx context.Context, key, uploadID string) error {
	_, err := s.storage.Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(s.storage.Bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
	})
	if err != nil && !isS3Missing(err) {
		return fmt.Errorf("abort multipart upload: %w", err)
	}
	return nil
}

func (s *server) headObject(ctx context.Context, key string) (objectMetadata, error) {
	result, err := s.storage.Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.storage.Bucket), Key: aws.String(key),
	})
	if err != nil {
		return objectMetadata{}, err
	}
	if result.ContentLength == nil {
		return objectMetadata{}, errors.New("head object returned no content length")
	}
	return objectMetadata{
		SizeBytes:   *result.ContentLength,
		ETag:        aws.ToString(result.ETag),
		ContentType: aws.ToString(result.ContentType),
	}, nil
}

func (s *server) deleteObject(ctx context.Context, key string) error {
	_, err := s.storage.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.storage.Bucket), Key: aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

func (s *server) presignPreview(ctx context.Context, key string) (string, time.Time, error) {
	result, err := s.storage.Presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.storage.Bucket), Key: aws.String(key),
	}, s.presignOptions)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign get object: %w", err)
	}
	return result.URL, time.Now().Add(s.storage.PresignTTL()), nil
}

func (s *server) presignDownload(
	ctx context.Context,
	key, filename string,
) (string, time.Time, error) {
	disposition := fmt.Sprintf(`attachment; filename="%s"`, filename)
	result, err := s.storage.Presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.storage.Bucket), Key: aws.String(key),
		ResponseContentType:        aws.String("video/mp4"),
		ResponseContentDisposition: aws.String(disposition),
	}, s.presignOptions)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign download object: %w", err)
	}
	return result.URL, time.Now().Add(s.storage.PresignTTL()), nil
}

func (s *server) presignOptions(options *s3.PresignOptions) {
	options.Expires = s.storage.PresignTTL()
}

func isS3Missing(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "NotFound", "NoSuchKey", "NoSuchUpload", "NoSuchBucket":
		return true
	default:
		return false
	}
}
