package main

import (
	"testing"
	"time"
)

func TestParseProbeSelectsVideoAndAudioMetadata(t *testing.T) {
	probe, err := parseProbe([]byte(`{
  "format": {"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"61.234"},
  "streams": [
    {"codec_type":"video","codec_name":"h264","width":1920,"height":1080},
    {"codec_type":"audio","codec_name":"aac"}
  ]
}`), 2*time.Hour)
	if err != nil {
		t.Fatalf("parseProbe: %v", err)
	}
	if probe.DurationMS != 61234 || probe.Width != 1920 || probe.Height != 1080 ||
		probe.VideoCodec != "h264" || probe.AudioCodec != "aac" || probe.ContentType != "video/mp4" {
		t.Fatalf("probe = %+v", probe)
	}
}

func TestParseProbeRejectsSilentAndOverlongVideo(t *testing.T) {
	silent := []byte(`{"format":{"duration":"10"},"streams":[{"codec_type":"video","codec_name":"h264","width":10,"height":10}]}`)
	if _, err := parseProbe(silent, time.Hour); err == nil {
		t.Fatal("silent video was accepted")
	}
	overlong := []byte(`{"format":{"duration":"7201"},"streams":[{"codec_type":"video","codec_name":"h264","width":10,"height":10},{"codec_type":"audio","codec_name":"aac"}]}`)
	if _, err := parseProbe(overlong, 2*time.Hour); err == nil {
		t.Fatal("overlong video was accepted")
	}
}
