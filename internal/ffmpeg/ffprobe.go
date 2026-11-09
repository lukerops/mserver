// Package ffmpeg wraps the ffmpeg command line tools. Results are plain Go
// types, free of the concepts of any client API.
package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ErrInvalidMedia is returned when ffprobe cannot read a file (e.g. it is
// not a media file or it is corrupted). Retrying does not help until the
// file changes.
var ErrInvalidMedia = errors.New("ffmpeg: invalid media")

// Stream types.
const (
	StreamVideo    = "video"
	StreamAudio    = "audio"
	StreamSubtitle = "subtitle"
)

// Probe inspects the file at path. It returns ErrInvalidMedia when ffprobe
// runs but cannot read the file; any other error (e.g. ffprobe is not
// installed) is worth retrying later.
func (p Prober) Probe(ctx context.Context, path string) (*Probe, error) {
	bin := p.Path
	if bin == "" {
		bin = "ffprobe"
	}
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"--", path,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case errors.As(err, &exitErr):
		return nil, fmt.Errorf("ffmpeg: %w: %s", ErrInvalidMedia, strings.TrimSpace(stderr.String()))
	case err != nil:
		return nil, fmt.Errorf("ffmpeg: run ffprobe: %w", err)
	}
	return Parse(stdout.Bytes())
}

// Parse parses the JSON output of
// "ffprobe -print_format json -show_format -show_streams".
func Parse(data []byte) (*Probe, error) {
	var out probeOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w: parse ffprobe output: %w", ErrInvalidMedia, err)
	}
	if out.Format.FormatName == "" {
		return nil, fmt.Errorf("ffmpeg: %w: no format", ErrInvalidMedia)
	}

	p := Probe{
		Container: out.Format.FormatName,
		Duration:  seconds(out.Format.Duration),
		BitRate:   integer(out.Format.BitRate),
		Size:      integer(out.Format.Size),
		Streams:   []Stream{},
	}
	for _, s := range out.Streams {
		switch s.CodecType {
		case StreamVideo:
			if s.Disposition["attached_pic"] == 1 {
				continue // cover art
			}
		case StreamAudio, StreamSubtitle:
		default:
			continue
		}
		p.Streams = append(p.Streams, s.toStream())
	}
	return &p, nil
}

// pixelFormatDepth guesses the bit depth from the pixel format name (e.g.
// "yuv420p10le" is 10 bits).
func pixelFormatDepth(pixFmt string) int {
	switch {
	case pixFmt == "":
		return 0
	case strings.Contains(pixFmt, "p10"):
		return 10
	case strings.Contains(pixFmt, "p12"):
		return 12
	default:
		return 8
	}
}

func integer(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func seconds(s string) time.Duration {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return time.Duration(math.Round(f * float64(time.Second)))
}

// rational parses "num/den" (e.g. "24000/1001").
func rational(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || d == 0 {
		return 0
	}
	return n / d
}
