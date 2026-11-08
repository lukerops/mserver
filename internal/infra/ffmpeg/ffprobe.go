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

// Probe is the technical information of a media file.
type Probe struct {
	// Container is the format name reported by ffprobe (e.g.
	// "matroska,webm" or "mov,mp4,m4a,3gp,3g2,mj2").
	Container string
	Duration  time.Duration
	// BitRate is the overall bit rate in bits per second.
	BitRate int64
	// Size is the file size in bytes.
	Size int64
	// Streams are the video, audio and subtitle streams, in file order.
	// Other streams (attachments, data and cover art) are left out.
	Streams []Stream
}

// Stream is a video, audio or subtitle stream. Fields that do not apply to
// the stream type are zero.
type Stream struct {
	// Index is the index of the stream in the file.
	Index int
	// Type is StreamVideo, StreamAudio or StreamSubtitle.
	Type    string
	Codec   string
	Profile string
	Level   int
	// Language is the language tag of the stream, as written in the file
	// (usually ISO 639-2, e.g. "eng").
	Language        string
	Title           string
	Default         bool
	Forced          bool
	HearingImpaired bool
	// BitRate is in bits per second.
	BitRate int64

	// Video.
	Width          int
	Height         int
	AspectRatio    string
	FrameRate      float64
	PixelFormat    string
	BitDepth       int
	Interlaced     bool
	ColorSpace     string
	ColorTransfer  string
	ColorPrimaries string

	// Audio.
	Channels      int
	ChannelLayout string
	SampleRate    int
}

// Prober runs ffprobe.
type Prober struct {
	// Path is the ffprobe binary, looked up in PATH when it has no
	// slash. Empty means "ffprobe".
	Path string
}

// Probe inspects the file at path. It returns ErrInvalidMedia when ffprobe
// runs but cannot read the file; any other error (e.g. ffprobe is not
// installed) is worth retrying later.
func (p Prober) Probe(ctx context.Context, path string) (Probe, error) {
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
		return Probe{}, ctx.Err()
	case errors.As(err, &exitErr):
		return Probe{}, fmt.Errorf("%w: %s", ErrInvalidMedia, strings.TrimSpace(stderr.String()))
	case err != nil:
		return Probe{}, fmt.Errorf("ffmpeg: run ffprobe: %w", err)
	}
	return Parse(stdout.Bytes())
}

// Parse parses the JSON output of
// "ffprobe -print_format json -show_format -show_streams".
func Parse(data []byte) (Probe, error) {
	var out probeOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return Probe{}, fmt.Errorf("%w: parse ffprobe output: %w", ErrInvalidMedia, err)
	}
	if out.Format.FormatName == "" {
		return Probe{}, fmt.Errorf("%w: no format", ErrInvalidMedia)
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
	return p, nil
}

// probeOutput is the JSON written by ffprobe. Numbers that may not fit or
// be absent are strings.
type probeOutput struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams []probeStream `json:"streams"`
}

type probeStream struct {
	Index              int               `json:"index"`
	CodecName          string            `json:"codec_name"`
	CodecType          string            `json:"codec_type"`
	Profile            string            `json:"profile"`
	Level              int               `json:"level"`
	Width              int               `json:"width"`
	Height             int               `json:"height"`
	DisplayAspectRatio string            `json:"display_aspect_ratio"`
	PixFmt             string            `json:"pix_fmt"`
	FieldOrder         string            `json:"field_order"`
	ColorSpace         string            `json:"color_space"`
	ColorTransfer      string            `json:"color_transfer"`
	ColorPrimaries     string            `json:"color_primaries"`
	AvgFrameRate       string            `json:"avg_frame_rate"`
	RFrameRate         string            `json:"r_frame_rate"`
	BitsPerRawSample   string            `json:"bits_per_raw_sample"`
	BitRate            string            `json:"bit_rate"`
	SampleRate         string            `json:"sample_rate"`
	Channels           int               `json:"channels"`
	ChannelLayout      string            `json:"channel_layout"`
	Disposition        map[string]int    `json:"disposition"`
	Tags               map[string]string `json:"tags"`
}

func (s probeStream) toStream() Stream {
	tags := make(map[string]string, len(s.Tags))
	for k, v := range s.Tags {
		tags[strings.ToLower(k)] = v
	}

	out := Stream{
		Index:           s.Index,
		Type:            s.CodecType,
		Codec:           s.CodecName,
		Profile:         s.Profile,
		Language:        tags["language"],
		Title:           tags["title"],
		Default:         s.Disposition["default"] == 1,
		Forced:          s.Disposition["forced"] == 1,
		HearingImpaired: s.Disposition["hearing_impaired"] == 1,
		BitRate:         integer(s.BitRate),
	}
	if out.BitRate == 0 {
		// Matroska keeps the stream bit rate in the BPS tag.
		out.BitRate = integer(tags["bps"])
		if out.BitRate == 0 {
			out.BitRate = integer(tags["bps-eng"])
		}
	}
	if out.Language == "und" {
		out.Language = ""
	}

	switch s.CodecType {
	case StreamVideo:
		out.Level = s.Level
		out.Width = s.Width
		out.Height = s.Height
		if s.DisplayAspectRatio != "0:1" {
			out.AspectRatio = s.DisplayAspectRatio
		}
		out.FrameRate = rational(s.AvgFrameRate)
		if out.FrameRate == 0 {
			out.FrameRate = rational(s.RFrameRate)
		}
		out.PixelFormat = s.PixFmt
		out.BitDepth = int(integer(s.BitsPerRawSample))
		if out.BitDepth == 0 {
			out.BitDepth = pixelFormatDepth(s.PixFmt)
		}
		out.Interlaced = s.FieldOrder != "" && s.FieldOrder != "progressive" && s.FieldOrder != "unknown"
		out.ColorSpace = s.ColorSpace
		out.ColorTransfer = s.ColorTransfer
		out.ColorPrimaries = s.ColorPrimaries
	case StreamAudio:
		out.Channels = s.Channels
		out.ChannelLayout = s.ChannelLayout
		out.SampleRate = int(integer(s.SampleRate))
	}
	return out
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
