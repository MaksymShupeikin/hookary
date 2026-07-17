package shared

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const TranscriptVersion = 1

type TranscriptDocument struct {
	Version    int                 `json:"version"`
	Source     string              `json:"source"`
	Language   string              `json:"language,omitempty"`
	DurationMS int64               `json:"duration_ms"`
	Text       string              `json:"text"`
	Words      []TranscriptWord    `json:"words"`
	Cues       []TranscriptCue     `json:"cues"`
	Segments   []TranscriptSegment `json:"segments"`
}

type TranscriptWord struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

type TranscriptCue struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

type TranscriptSegment struct {
	ID      string `json:"id"`
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

func NewTranscriptDocument(
	source, language string,
	durationMS int64,
	words []TranscriptWord,
	cues []TranscriptCue,
) TranscriptDocument {
	words = validWords(words)
	if len(cues) == 0 {
		cues = cuesFromWords(words)
	} else {
		cues = validCues(cues)
	}
	if durationMS <= 0 {
		if len(cues) > 0 {
			durationMS = cues[len(cues)-1].EndMS
		} else if len(words) > 0 {
			durationMS = words[len(words)-1].EndMS
		}
	}
	return TranscriptDocument{
		Version: TranscriptVersion, Source: source, Language: language,
		DurationMS: durationMS, Text: textFromCues(cues), Words: words,
		Cues: cues, Segments: segmentsFromCues(cues),
	}
}

func validWords(words []TranscriptWord) []TranscriptWord {
	result := make([]TranscriptWord, 0, len(words))
	for _, word := range words {
		word.Text = strings.TrimSpace(word.Text)
		if word.Text == "" || word.StartMS < 0 || word.EndMS <= word.StartMS {
			continue
		}
		result = append(result, word)
	}
	return result
}

func validCues(cues []TranscriptCue) []TranscriptCue {
	result := make([]TranscriptCue, 0, len(cues))
	for _, cue := range cues {
		cue.Text = cleanTranscriptText(cue.Text)
		if cue.Text == "" || cue.StartMS < 0 || cue.EndMS <= cue.StartMS {
			continue
		}
		result = append(result, cue)
	}
	return result
}

func cuesFromWords(words []TranscriptWord) []TranscriptCue {
	const (
		minimumCueMS = int64(1800)
		maximumCueMS = int64(6000)
		maximumWords = 14
	)
	var cues []TranscriptCue
	for start := 0; start < len(words); {
		end := start
		for end+1 < len(words) {
			duration := words[end].EndMS - words[start].StartMS
			if duration >= maximumCueMS || end-start+1 >= maximumWords {
				break
			}
			if duration >= minimumCueMS && endsSentence(words[end].Text) {
				break
			}
			end++
		}
		cues = append(cues, TranscriptCue{
			StartMS: words[start].StartMS,
			EndMS:   words[end].EndMS,
			Text:    joinTranscriptParts(words[start : end+1]),
		})
		start = end + 1
	}
	return validCues(cues)
}

func segmentsFromCues(cues []TranscriptCue) []TranscriptSegment {
	const maximumSegmentMS = int64(30_000)
	var result []TranscriptSegment
	for start := 0; start < len(cues); {
		end := start
		for end+1 < len(cues) && cues[end+1].EndMS-cues[start].StartMS <= maximumSegmentMS {
			end++
		}
		texts := make([]string, 0, end-start+1)
		for _, cue := range cues[start : end+1] {
			texts = append(texts, cue.Text)
		}
		result = append(result, TranscriptSegment{
			ID:      segmentID(len(result) + 1),
			StartMS: cues[start].StartMS,
			EndMS:   cues[end].EndMS,
			Text:    cleanTranscriptText(strings.Join(texts, " ")),
		})
		start = end + 1
	}
	return result
}

func textFromCues(cues []TranscriptCue) string {
	parts := make([]string, 0, len(cues))
	for _, cue := range cues {
		parts = append(parts, cue.Text)
	}
	return cleanTranscriptText(strings.Join(parts, " "))
}

func joinTranscriptParts(words []TranscriptWord) string {
	parts := make([]string, 0, len(words))
	for _, word := range words {
		parts = append(parts, word.Text)
	}
	return cleanTranscriptText(strings.Join(parts, " "))
}

func cleanTranscriptText(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\n", " ")), " ")
}

func endsSentence(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(value)
	return strings.ContainsRune(".!?…", last)
}

func segmentID(index int) string {
	const digits = "0123456789"
	buffer := []byte{'s', '0', '0', '0', '0'}
	for position := len(buffer) - 1; position > 0 && index > 0; position-- {
		buffer[position] = digits[index%10]
		index /= 10
	}
	return string(buffer)
}

func IsLikelySpeech(value string) bool {
	for _, current := range value {
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			return true
		}
	}
	return false
}
