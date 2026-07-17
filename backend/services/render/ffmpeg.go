package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/MaksymShupeikin/hookary/backend/db"
)

type renderedProbe struct {
	Streams []struct {
		Width  int32 `json:"width"`
		Height int32 `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func runVerticalRender(
	ctx context.Context,
	cfg renderConfig,
	workDir string,
	clip db.Clip,
) error {
	filter, complex := renderVideoFilter(clip)
	duration := float64(clip.EndMs-clip.StartMs) / 1000
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-ss", seconds(clip.StartMs), "-i", "source.mp4",
		"-t", strconv.FormatFloat(duration, 'f', 3, 64),
	}
	if complex {
		args = append(args, "-filter_complex", filter, "-map", "[video]")
	} else {
		args = append(args, "-vf", filter, "-map", "0:v:0")
	}
	args = append(args,
		"-map", "0:a:0?",
		"-af", "loudnorm=I=-16:TP=-1.5:LRA=11",
		"-c:v", "libx264", "-preset", "medium", "-crf", "20",
		"-pix_fmt", "yuv420p", "-r", "30",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		"-threads", strconv.Itoa(cfg.Threads),
		"output.mp4",
	)
	if output, err := runMediaCommand(ctx, workDir, cfg.FFmpegPath, args...); err != nil {
		return fmt.Errorf("render vertical clip: %w: %s", err, output)
	}
	return nil
}

func renderVideoFilter(clip db.Clip) (string, bool) {
	subtitles := "subtitles=filename=subtitles.ass:fontsdir=/usr/share/fonts"
	// Keep the foreground in a full-width horizontal 16:9 window. A blurred,
	// darkened copy fills the space above and below with the current frame's
	// dominant colours; the vignette carries that ambient fill into black.
	filter := "[0:v]split=2[background][foreground];" +
		"[background]scale=1080:1920:force_original_aspect_ratio=increase," +
		"crop=1080:1920,setsar=1,gblur=sigma=42:steps=2," +
		"eq=brightness=-0.30:saturation=1.20,vignette=PI/4[ambient];" +
		"[foreground]scale=1080:608:force_original_aspect_ratio=decrease," +
		"pad=1080:608:(ow-iw)/2:(oh-ih)/2:color=black,setsar=1[wide];" +
		"[ambient][wide]overlay=(W-w)/2:(H-h)/2:shortest=1," + subtitles +
		",fps=30,format=yuv420p[video]"
	return filter, true
}

func createThumbnail(ctx context.Context, cfg renderConfig, workDir string) error {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-ss", "1", "-i", "output.mp4", "-frames:v", "1",
		"-vf", "scale=360:640", "-c:v", "libwebp", "-quality", "82",
		"thumbnail.webp",
	}
	if output, err := runMediaCommand(ctx, workDir, cfg.FFmpegPath, args...); err != nil {
		return fmt.Errorf("create thumbnail: %w: %s", err, output)
	}
	return nil
}

func validateRenderedOutput(
	ctx context.Context,
	cfg renderConfig,
	workDir string,
	expectedDurationMS int64,
) error {
	args := []string{
		"-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration",
		"-of", "json", "output.mp4",
	}
	output, err := runMediaCommand(ctx, workDir, cfg.FFprobePath, args...)
	if err != nil {
		return fmt.Errorf("probe rendered clip: %w: %s", err, output)
	}
	var probe renderedProbe
	if err := json.Unmarshal([]byte(output), &probe); err != nil {
		return fmt.Errorf("decode rendered probe: %w", err)
	}
	if len(probe.Streams) != 1 ||
		probe.Streams[0].Width != 1080 || probe.Streams[0].Height != 1920 {
		return errors.New("rendered clip does not have 1080x1920 video")
	}
	duration, err := strconv.ParseFloat(probe.Format.Duration, 64)
	if err != nil {
		return errors.New("rendered clip has no readable duration")
	}
	expected := float64(expectedDurationMS) / 1000
	if duration < expected-1.5 || duration > expected+1.5 {
		return fmt.Errorf("rendered duration %.3fs differs from expected %.3fs", duration, expected)
	}
	return nil
}

func runMediaCommand(
	ctx context.Context,
	workDir, binary string,
	args ...string,
) (string, error) {
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = workDir
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	return strings.TrimSpace(output.String()), err
}

func seconds(milliseconds int64) string {
	return strconv.FormatFloat(float64(milliseconds)/1000, 'f', 3, 64)
}
