// Package ffprobe is the storage analyzer that extracts the technical
// information of video files (container, duration, bit rate and streams)
// with ffprobe. Its results are events.FFprobeResultV1.
package ffprobe

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

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
}

var _ analyzer.Analyzer = (*Analyzer)(nil)

// New returns the analyzer. binary is the ffprobe executable; empty means
// "ffprobe" from PATH.
func New(binary string) *Analyzer {
	return &Analyzer{prober: ffmpeg.Prober{Path: binary}}
}

func (*Analyzer) Name() string { return Name }
func (*Analyzer) Version() int { return Version }

func (*Analyzer) Accepts(p string) bool {
	return videoExtensions[strings.ToLower(path.Ext(p))]
}

func (a *Analyzer) Analyze(ctx context.Context, localPath string) (any, error) {
	p, err := a.prober.Probe(ctx, localPath)
	if errors.Is(err, ffmpeg.ErrInvalidMedia) {
		return nil, fmt.Errorf("%w: %w", analyzer.ErrInvalidFile, err)
	}
	if err != nil {
		return nil, err
	}
	return toResult(p), nil
}

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
