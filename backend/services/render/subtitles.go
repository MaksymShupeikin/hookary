package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MaksymShupeikin/hookary/backend/shared"
)

const assHeader = `[Script Info]
ScriptType: v4.00+
PlayResX: 1080
PlayResY: 1920
WrapStyle: 2
ScaledBorderAndShadow: yes

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Noto Sans,72,&H00FFFFFF,&H000000FF,&H00101010,&H80000000,-1,0,0,0,100,100,0,0,1,5,2,2,70,70,330,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
`

func writeClipSubtitles(
	path string,
	document shared.TranscriptDocument,
	clipStartMS, clipEndMS int64,
) (int, error) {
	var events strings.Builder
	count := 0
	for _, cue := range document.Cues {
		start := max(cue.StartMS, clipStartMS)
		end := min(cue.EndMS, clipEndMS)
		if end <= start || !shared.IsLikelySpeech(cue.Text) {
			continue
		}
		fmt.Fprintf(&events, "Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n",
			assTimestamp(start-clipStartMS), assTimestamp(end-clipStartMS),
			escapeASSText(cue.Text),
		)
		count++
	}
	if count == 0 {
		return 0, nil
	}
	if err := os.WriteFile(path, []byte(assHeader+events.String()), 0o600); err != nil {
		return 0, fmt.Errorf("write ASS subtitles: %w", err)
	}
	return count, nil
}

func assTimestamp(milliseconds int64) string {
	if milliseconds < 0 {
		milliseconds = 0
	}
	duration := time.Duration(milliseconds) * time.Millisecond
	hours := int(duration / time.Hour)
	minutes := int(duration/time.Minute) % 60
	seconds := int(duration/time.Second) % 60
	centiseconds := int(duration/(10*time.Millisecond)) % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", hours, minutes, seconds, centiseconds)
}

func escapeASSText(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "{", `\{`)
	value = strings.ReplaceAll(value, "}", `\}`)
	value = strings.ReplaceAll(value, "\r\n", `\N`)
	value = strings.ReplaceAll(value, "\n", `\N`)
	value = strings.ReplaceAll(value, "\r", `\N`)
	return strings.TrimSpace(value)
}
