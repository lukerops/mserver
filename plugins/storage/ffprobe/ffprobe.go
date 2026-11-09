// Package ffprobe is the storage analyzer that extracts the technical
// information of video files (container, duration, bit rate and streams)
// with ffprobe. Its results are events.FFprobeResultV1.
package ffprobe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/lukerops/mserver/internal/infra/ffmpeg"
	"github.com/lukerops/mserver/internal/modules/storage/analyzer"
	"github.com/lukerops/mserver/internal/modules/storage/events"
)

const (
	// Name of the analyzer in the storage.file.analyzed.v1 events.
	Name = events.AnalyzerFFprobe
	// Version of the result: events.FFprobeResultV1.
	Version = 1
)

// videoExtensions are the files inspected by the analyzer.
var videoExtensions = map[string]bool{
	".3gp": true, ".asf": true, ".avi": true, ".divx": true, ".flv": true,
	".m2ts": true, ".m4v": true, ".mkv": true, ".mov": true, ".mp4": true,
	".mpeg": true, ".mpg": true, ".mts": true, ".ogm": true, ".ogv": true,
	".rmvb": true, ".ts": true, ".vob": true, ".webm": true, ".wmv": true,
}

// Analyzer runs ffprobe on video files.
type Analyzer struct {
	prober ffmpeg.Prober
	log    *slog.Logger
}

var _ analyzer.Analyzer = (*Analyzer)(nil)

// maxErrorLen bounds the ffprobe output kept in errors, which end up in
// logs and in the saved analyses.
const maxErrorLen = 512

// New returns the analyzer. binary is the ffprobe executable; empty means
// "ffprobe" from PATH. log may be nil.
func New(binary string, log *slog.Logger) *Analyzer {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Analyzer{prober: ffmpeg.Prober{Path: binary}, log: log}
}

func (*Analyzer) Name() string { return Name }
func (*Analyzer) Version() int { return Version }

func (*Analyzer) Accepts(p string) bool {
	return videoExtensions[strings.ToLower(path.Ext(p))]
}

func (a *Analyzer) Analyze(ctx context.Context, localPath string) (any, error) {
	start := time.Now()
	p, err := a.prober.Probe(ctx, localPath)
	a.log.DebugContext(ctx, "storage: ffprobe run", "binary", a.binary(), "file", localPath,
		"duration", time.Since(start), "ok", err == nil)
	if errors.Is(err, ffmpeg.ErrInvalidMedia) {
		return nil, fmt.Errorf("%w: %w", analyzer.ErrInvalidFile, truncatedError{err})
	}
	if err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w", a.binary(), truncatedError{err})
	}
	return toResult(p), nil
}

func (a *Analyzer) binary() string {
	if a.prober.Path == "" {
		return "ffprobe"
	}
	return a.prober.Path
}

// truncatedError shortens the message of an error carrying the ffprobe
// output to maxErrorLen bytes, keeping it for errors.Is.
type truncatedError struct{ err error }

func (e truncatedError) Error() string {
	msg := strings.Join(strings.Fields(e.err.Error()), " ")
	if len(msg) <= maxErrorLen {
		return msg
	}
	return strings.ToValidUTF8(msg[:maxErrorLen], "") + "..."
}

func (e truncatedError) Unwrap() error { return e.err }

func toResult(p ffmpeg.Probe) events.FFprobeResultV1 {
	r := events.FFprobeResultV1{
		Container:  p.Container,
		DurationMs: p.Duration.Milliseconds(),
		BitRate:    p.BitRate,
		Size:       p.Size,
		Streams:    make([]events.FFprobeStreamV1, len(p.Streams)),
	}
	for i, s := range p.Streams {
		r.Streams[i] = events.FFprobeStreamV1{
			Index:           s.Index,
			Type:            s.Type,
			Codec:           s.Codec,
			Profile:         s.Profile,
			Level:           s.Level,
			Language:        s.Language,
			Title:           s.Title,
			Default:         s.Default,
			Forced:          s.Forced,
			HearingImpaired: s.HearingImpaired,
			BitRate:         s.BitRate,
			Width:           s.Width,
			Height:          s.Height,
			AspectRatio:     s.AspectRatio,
			FrameRate:       s.FrameRate,
			PixelFormat:     s.PixelFormat,
			BitDepth:        s.BitDepth,
			Interlaced:      s.Interlaced,
			ColorSpace:      s.ColorSpace,
			ColorTransfer:   s.ColorTransfer,
			ColorPrimaries:  s.ColorPrimaries,
			Channels:        s.Channels,
			ChannelLayout:   s.ChannelLayout,
			SampleRate:      s.SampleRate,
		}
	}
	return r
}
