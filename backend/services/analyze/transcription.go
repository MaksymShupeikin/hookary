package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/openai/openai-go/v3"

	"github.com/MaksymShupeikin/hookary/backend/db"
	"github.com/MaksymShupeikin/hookary/backend/shared"
)

const whisperCostPerMinuteUSD = 0.006

func (p *analyzeProcessor) transcribeChunks(
	ctx context.Context,
	job db.Job,
	chunks []audioChunk,
	workDir string,
	report shared.ProgressReporter,
) (shared.TranscriptDocument, error) {
	words := make([]shared.TranscriptWord, 0, len(chunks)*2000)
	cues := make([]shared.TranscriptCue, 0, len(chunks)*200)
	language := ""
	var durationMS int64
	for index, chunk := range chunks {
		chunkDocument, cached, err := p.cachedChunk(ctx, job.ID, index)
		if err != nil {
			return shared.TranscriptDocument{}, retry("transcript_cache_failed", "Could not read the transcript cache.")
		}
		if !cached {
			chunkDocument, err = p.transcribeChunk(ctx, job.ID, index, chunk, workDir)
			if err != nil {
				return shared.TranscriptDocument{}, err
			}
		}
		if language == "" {
			language = chunkDocument.Language
		}
		for _, word := range chunkDocument.Words {
			word.StartMS += chunk.StartMS
			word.EndMS += chunk.StartMS
			words = append(words, word)
		}
		for _, cue := range chunkDocument.Cues {
			cue.StartMS += chunk.StartMS
			cue.EndMS += chunk.StartMS
			cues = append(cues, cue)
		}
		durationMS = max(durationMS, chunk.StartMS+chunk.DurationMS)
		progress := 0.05 + 0.8*float32(index+1)/float32(len(chunks))
		if err := report(progress); err != nil {
			return shared.TranscriptDocument{}, err
		}
	}
	if len(words) == 0 && len(cues) == 0 {
		return shared.TranscriptDocument{}, reject("speech_not_found", "No speech was found in the source audio.")
	}
	return shared.NewTranscriptDocument(
		string(p.cfg.TranscribeModel), language, durationMS, words, cues,
	), nil
}

func (p *analyzeProcessor) transcribeChunk(
	ctx context.Context,
	jobID string,
	index int,
	chunk audioChunk,
	workDir string,
) (shared.TranscriptDocument, error) {
	estimatedCost := float64(chunk.DurationMS) / 60_000 * whisperCostPerMinuteUSD
	totalCost, err := p.q.TotalAICostUSD(ctx)
	if err != nil {
		return shared.TranscriptDocument{}, retry("database_unavailable", "Could not check the AI budget.")
	}
	if totalCost+estimatedCost > p.cfg.BudgetLimitUSD {
		return shared.TranscriptDocument{}, reject("budget_exhausted", "The AI budget has been exhausted.")
	}

	audioPath := filepath.Join(workDir, fmt.Sprintf("audio-%04d.mp3", index))
	if err := downloadAudioChunk(ctx, p.storage, chunk, audioPath); err != nil {
		return shared.TranscriptDocument{}, retry("audio_unavailable", "Prepared audio is unavailable.")
	}
	file, err := os.Open(audioPath)
	if err != nil {
		return shared.TranscriptDocument{}, retry("work_storage_unavailable", "Could not open prepared audio.")
	}
	requestContext, cancel := context.WithTimeout(ctx, p.cfg.RequestTimeout)
	response, requestErr := p.client.Audio.Transcriptions.New(
		requestContext,
		openai.AudioTranscriptionNewParams{
			File: file, Model: p.cfg.TranscribeModel,
			ResponseFormat:         openai.AudioResponseFormatVerboseJSON,
			TimestampGranularities: []string{"word", "segment"},
		},
	)
	cancel()
	closeErr := file.Close()
	_ = os.Remove(audioPath)
	if requestErr != nil {
		return shared.TranscriptDocument{}, retry("transcription_failed", "OpenAI could not transcribe the source audio.")
	}
	if closeErr != nil {
		return shared.TranscriptDocument{}, retry("work_storage_unavailable", "Could not close prepared audio.")
	}
	verbose := response.AsTranscriptionVerbose()
	words := make([]shared.TranscriptWord, 0, len(verbose.Words))
	for _, word := range verbose.Words {
		words = append(words, shared.TranscriptWord{
			StartMS: secondsToMilliseconds(word.Start),
			EndMS:   secondsToMilliseconds(word.End),
			Text:    word.Word,
		})
	}
	cues := make([]shared.TranscriptCue, 0, len(verbose.Segments))
	for _, segment := range verbose.Segments {
		cues = append(cues, shared.TranscriptCue{
			StartMS: secondsToMilliseconds(segment.Start),
			EndMS:   secondsToMilliseconds(segment.End),
			Text:    segment.Text,
		})
	}
	durationMS := chunk.DurationMS
	if verbose.Duration > 0 {
		durationMS = secondsToMilliseconds(verbose.Duration)
	}
	document := shared.NewTranscriptDocument(
		string(p.cfg.TranscribeModel), verbose.Language, durationMS, words, cues,
	)
	if err := p.cacheChunk(ctx, jobID, index, document, workDir); err != nil {
		return shared.TranscriptDocument{}, retry("transcript_cache_failed", "Could not store the transcript cache.")
	}
	seconds := verbose.Usage.Seconds
	if seconds <= 0 {
		seconds = float64(chunk.DurationMS) / 1000
	}
	actualCost := seconds / 60 * whisperCostPerMinuteUSD
	model := string(p.cfg.TranscribeModel)
	if err := p.q.AddJobAICost(ctx, db.AddJobAICostParams{
		CostUsd: actualCost, Model: &model, JobID: jobID,
	}); err != nil {
		return shared.TranscriptDocument{}, retry("database_unavailable", "Could not record transcription usage.")
	}
	return document, nil
}

func (p *analyzeProcessor) cachedChunk(
	ctx context.Context,
	jobID string,
	index int,
) (shared.TranscriptDocument, bool, error) {
	document, err := shared.DownloadTranscript(ctx, p.storage, chunkTranscriptKey(jobID, index))
	if err != nil {
		return shared.TranscriptDocument{}, false, nil
	}
	if document.Source != string(p.cfg.TranscribeModel) {
		return shared.TranscriptDocument{}, false, nil
	}
	return document, true, nil
}

func (p *analyzeProcessor) cacheChunk(
	ctx context.Context,
	jobID string,
	index int,
	document shared.TranscriptDocument,
	workDir string,
) error {
	path := filepath.Join(workDir, fmt.Sprintf("transcript-%04d.json.gz", index))
	checksum, size, err := shared.WriteTranscriptFile(path, document)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	return shared.UploadTranscript(ctx, p.storage, chunkTranscriptKey(jobID, index), path, checksum, size)
}

func chunkTranscriptKey(jobID string, index int) string {
	return fmt.Sprintf("work/%s/transcript/chunks/%04d.json.gz", jobID, index)
}

func secondsToMilliseconds(seconds float64) int64 {
	return int64(math.Round(seconds * 1000))
}
