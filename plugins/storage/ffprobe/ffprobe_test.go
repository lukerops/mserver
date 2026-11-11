package ffprobe

import (
	"testing"
	"time"

	"github.com/lukerops/mserver/internal/ffmpeg"
)

func TestAccepts(t *testing.T) {
	a := New("", nil)
	for p, want := range map[string]bool{
		"Movie (1999)/Movie.mkv": true,
		"movie.MP4":              true,
		"Movie/Movie.en.srt":     false,
		"Movie/poster.jpg":       false,
		"README":                 false,
	} {
		if got := a.Accepts(p); got != want {
			t.Errorf("Accepts(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestToResult(t *testing.T) {
	r := toResult(&ffmpeg.Probe{
		Container: "matroska,webm",
		Duration:  90*time.Minute + 1500*time.Microsecond,
		BitRate:   8_000_000,
		Size:      5_400_000_000,
		Streams: []ffmpeg.Stream{
			{Index: 0, Type: ffmpeg.StreamVideo, Codec: "h264", Width: 1920, Height: 1080},
			{Index: 1, Type: ffmpeg.StreamAudio, Codec: "aac", Language: "eng", Channels: 2},
		},
	})
	if r.DurationMs != 90*60*1000+1 || r.Container != "matroska,webm" || len(r.Streams) != 2 ||
		r.Streams[0].Width != 1920 || r.Streams[1].Language != "eng" || r.Streams[1].Channels != 2 {
		t.Errorf("result = %+v", r)
	}
}
