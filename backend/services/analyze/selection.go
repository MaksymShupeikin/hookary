package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	openaiShared "github.com/openai/openai-go/v3/shared"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

const (
	gptMiniInputCostPerToken  = 0.75 / 1_000_000
	gptMiniOutputCostPerToken = 4.50 / 1_000_000
	maximumSelectionTokens    = int64(1800)
)

type clipSelectionResponse struct {
	Clips []modelClipSelection `json:"clips"`
}

type modelClipSelection struct {
	WindowID    string  `json:"window_id"`
	Score       float64 `json:"score"`
	Hook        string  `json:"hook"`
	Title       string  `json:"title"`
	Reason      string  `json:"reason"`
	ContextRisk string  `json:"context_risk"`
}

func (p *analyzeProcessor) selectAndSaveClips(
	ctx context.Context,
	job db.Job,
	document shared.TranscriptDocument,
	report shared.ProgressReporter,
) error {
	if job.AssetID == nil {
		return reject("asset_missing", "The analyze job has no source asset.")
	}
	count, err := p.q.CountAssetClips(ctx, *job.AssetID)
	if err != nil {
		return retry("database_unavailable", "Could not check existing clip suggestions.")
	}
	if count > 0 {
		return report(0.99)
	}
	windows := buildCandidateWindows(document.Segments)
	if len(windows) == 0 {
		return reject("speech_not_found", "The transcript contains no selectable 20 to 60 second windows.")
	}
	if cached, ok := p.loadSelectionCache(ctx, job.ID); ok {
		clips := validateClipSelections(document, windows, cached.Clips)
		if len(clips) > 0 {
			if err := p.saveClipSuggestions(ctx, job, clips); err != nil {
				return retry("database_unavailable", "Could not save cached clip suggestions.")
			}
			return report(0.99)
		}
	}
	if p.cfg.APIKey == "" || p.cfg.APIKey == "CHANGE_ME" {
		return reject("ai_not_configured", "OpenAI is not configured for clip selection.")
	}
	if err := report(0.88); err != nil {
		return err
	}
	input, err := selectionInput(document, windows)
	if err != nil {
		return reject("transcript_invalid", "The transcript could not be prepared for clip selection.")
	}

	var clips []validatedClip
	var selectedResponse clipSelectionResponse
	for attempt := 0; attempt < 2; attempt++ {
		output, callErr := p.callClipSelection(ctx, job.ID, *job.AssetID, input)
		if callErr != nil {
			return callErr
		}
		var modelResponse clipSelectionResponse
		if json.Unmarshal([]byte(output), &modelResponse) == nil {
			validated := validateClipSelections(document, windows, modelResponse.Clips)
			if len(validated) > len(clips) {
				clips = validated
				selectedResponse = modelResponse
			}
			if len(clips) >= 3 {
				break
			}
		}
		input += "\nThe previous result did not contain three usable, non-overlapping windows. " +
			"Return a corrected result using only window_id values from the supplied candidate windows."
	}
	if len(clips) == 0 {
		return reject("clip_selection_invalid", "The model did not rank any valid candidate windows.")
	}
	if err := p.storeSelectionCache(ctx, job.ID, selectedResponse); err != nil {
		return retry("selection_cache_failed", "Could not cache clip suggestions.")
	}
	if err := p.saveClipSuggestions(ctx, job, clips); err != nil {
		return retry("database_unavailable", "Could not save clip suggestions.")
	}
	return report(0.99)
}

func (p *analyzeProcessor) callClipSelection(
	ctx context.Context,
	jobID, assetID, input string,
) (string, error) {
	estimatedInputTokens := int64(len(input)/4 + 1)
	estimatedCost := float64(estimatedInputTokens)*gptMiniInputCostPerToken +
		float64(maximumSelectionTokens)*gptMiniOutputCostPerToken
	totalCost, err := p.q.TotalAICostUSD(ctx)
	if err != nil {
		return "", retry("database_unavailable", "Could not check the AI budget.")
	}
	if totalCost+estimatedCost > p.cfg.BudgetLimitUSD {
		return "", reject("budget_exhausted", "The AI budget has been exhausted.")
	}
	format := responses.ResponseFormatTextConfigParamOfJSONSchema(
		"hookary_clip_selection", clipSelectionSchema(),
	)
	format.OfJSONSchema.Strict = openai.Bool(true)
	requestContext, cancel := context.WithTimeout(ctx, p.cfg.RequestTimeout)
	response, requestErr := p.client.Responses.New(requestContext, responses.ResponseNewParams{
		Model: openaiShared.ResponsesModel(p.cfg.SelectModel),
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String(input)},
		Instructions: openai.String(
			"You rank self-contained short-form video moments. Choose 3 to 5 strong, " +
				"non-overlapping candidate windows when the material supports it. Begin with a clear hook, " +
				"make sense without missing context, and preserve the transcript language. Select only " +
				"window_id values from the supplied candidate windows; never invent ids, timestamps, or words.",
		),
		MaxOutputTokens: openai.Int(maximumSelectionTokens),
		Reasoning: openaiShared.ReasoningParam{
			Effort: openaiShared.ReasoningEffortLow,
		},
		Text:  responses.ResponseTextConfigParam{Format: format},
		Store: openai.Bool(false), PromptCacheKey: openai.String(assetID),
	})
	cancel()
	if requestErr != nil {
		return "", retry("clip_selection_failed", "OpenAI could not select clip candidates.")
	}
	actualCost := float64(response.Usage.InputTokens)*gptMiniInputCostPerToken +
		float64(response.Usage.OutputTokens)*gptMiniOutputCostPerToken
	model, promptVersion := p.cfg.SelectModel, p.cfg.PromptVersion
	if err := p.q.AddJobAIUsage(ctx, db.AddJobAIUsageParams{
		InputTokens: response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens,
		CostUsd: actualCost, PromptVersion: &promptVersion, Model: &model, JobID: jobID,
	}); err != nil {
		return "", retry("database_unavailable", "Could not record clip-selection usage.")
	}
	output := strings.TrimSpace(response.OutputText())
	return output, nil
}

func selectionInput(
	document shared.TranscriptDocument,
	windows []candidateWindow,
) (string, error) {
	segmentsPayload, err := json.Marshal(document.Segments)
	if err != nil {
		return "", err
	}
	windowsPayload, err := json.Marshal(windows)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"Transcript language: %s\nVideo duration: %d ms\nTimed segments JSON:\n%s"+
			"\nValid candidate windows JSON:\n%s",
		document.Language, document.DurationMS, segmentsPayload, windowsPayload,
	), nil
}

func (p *analyzeProcessor) saveClipSuggestions(
	ctx context.Context,
	job db.Job,
	clips []validatedClip,
) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := p.q.WithTx(tx)
	model, promptVersion := p.cfg.SelectModel, p.cfg.PromptVersion
	for _, clip := range clips {
		candidateID := shared.NewID()
		hook, title, reason, contextRisk := clip.Hook, clip.Title, clip.Reason, clip.ContextRisk
		if _, err := q.CreateClipCandidate(ctx, db.CreateClipCandidateParams{
			ID: candidateID, ProjectID: job.ProjectID, AssetID: *job.AssetID,
			StartMs: clip.StartMS, EndMs: clip.EndMS, Score: clip.Score,
			Hook: &hook, Title: &title, Reason: &reason, ContextRisk: &contextRisk,
			Model: &model, PromptVersion: &promptVersion,
		}); err != nil {
			return err
		}
		if _, err := q.CreateClip(ctx, db.CreateClipParams{
			ID: shared.NewID(), ProjectID: job.ProjectID, AssetID: *job.AssetID,
			CandidateID: &candidateID, Title: title, Hook: &hook, Reason: &reason,
			Score: clip.Score, ContextRisk: &contextRisk,
			StartMs: clip.StartMS, EndMs: clip.EndMS,
		}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func clipSelectionSchema() map[string]any {
	clip := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{
			"window_id", "score", "hook", "title", "reason", "context_risk",
		},
		"properties": map[string]any{
			"window_id": map[string]any{"type": "string"},
			"score":     map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"hook":      map[string]any{"type": "string"},
			"title":     map[string]any{"type": "string"},
			"reason":    map[string]any{"type": "string"},
			"context_risk": map[string]any{
				"type": "string", "enum": []string{"low", "medium", "high"},
			},
		},
	}
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"clips"},
		"properties": map[string]any{
			"clips": map[string]any{
				"type": "array", "minItems": 1, "maxItems": 5, "items": clip,
			},
		},
	}
}
