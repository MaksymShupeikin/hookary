package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const maximumSelectionCacheBytes = int64(1 << 20)

const selectionCacheVersion = 2

type storedSelection struct {
	Version       int                  `json:"version"`
	Model         string               `json:"model"`
	PromptVersion string               `json:"prompt_version"`
	Clips         []modelClipSelection `json:"clips"`
}

func (p *analyzeProcessor) loadSelectionCache(
	ctx context.Context,
	jobID string,
) (storedSelection, bool) {
	object, err := p.storage.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(p.storage.Bucket), Key: aws.String(selectionCacheKey(jobID)),
	})
	if err != nil {
		return storedSelection{}, false
	}
	defer object.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(object.Body, maximumSelectionCacheBytes+1))
	if err != nil || int64(len(payload)) > maximumSelectionCacheBytes {
		return storedSelection{}, false
	}
	var stored storedSelection
	if json.Unmarshal(payload, &stored) != nil || stored.Version != selectionCacheVersion ||
		stored.Model != p.cfg.SelectModel || stored.PromptVersion != p.cfg.PromptVersion {
		return storedSelection{}, false
	}
	return stored, true
}

func (p *analyzeProcessor) storeSelectionCache(
	ctx context.Context,
	jobID string,
	selection clipSelectionResponse,
) error {
	payload, err := json.Marshal(storedSelection{
		Version: selectionCacheVersion, Model: p.cfg.SelectModel, PromptVersion: p.cfg.PromptVersion,
		Clips: selection.Clips,
	})
	if err != nil {
		return err
	}
	_, err = p.storage.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(p.storage.Bucket), Key: aws.String(selectionCacheKey(jobID)),
		Body: bytes.NewReader(payload), ContentLength: aws.Int64(int64(len(payload))),
		ContentType: aws.String("application/json"),
	})
	return err
}

func selectionCacheKey(jobID string) string {
	return "work/" + jobID + "/selection/clip-selection.json"
}
