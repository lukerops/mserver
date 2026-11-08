// Package ffmpegtest creates real media files for tests with the ffmpeg
// installed on the machine, so that they can be analyzed by the real
// ffprobe.
package ffmpegtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Movie describes the files written by WriteMovie, as reported by ffprobe.
const (
	MovieWidth    = 1920
	MovieHeight   = 1080
	MovieLanguage = "eng"
	// MovieMinDurationMs is the requested duration: the container reports
	// a little more (the last audio frame).
	MovieMinDurationMs = 1000
)

// FFprobe returns the path of ffprobe. It skips the test when ffmpeg or
// ffprobe are not installed.
func FFprobe(t testing.TB) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}
	return path
}

// WriteMovie writes at path (a .mkv) a 1 s movie: 1080p H.264 video
// followed by stereo AAC audio in English, the default audio stream. It
// skips the test when ffmpeg is not installed.
func WriteMovie(t testing.TB, path string) {
	t.Helper()
	FFprobe(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "ffmpeg",
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=1920x1080:r=24:d=1",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=1",
		"-map", "0:v", "-map", "1:a",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-ac", "2",
		"-metadata:s:a:0", "language="+MovieLanguage,
		"-disposition:a:0", "default",
		"-shortest", path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
}
