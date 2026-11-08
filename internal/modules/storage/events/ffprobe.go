package events

// AnalyzerFFprobe is the analyzer that extracts the technical information
// of video files (container, duration, bit rate and streams).
const AnalyzerFFprobe = "ffprobe"

// FFprobeResultV1 is the result of AnalyzerFFprobe version 1.
type FFprobeResultV1 struct {
	// Container is the format name reported by ffprobe (e.g.
	// "matroska,webm").
	Container  string `json:"container"`
	DurationMs int64  `json:"duration_ms"`
	// BitRate is in bits per second.
	BitRate int64             `json:"bit_rate,omitempty"`
	Size    int64             `json:"size,omitempty"`
	Streams []FFprobeStreamV1 `json:"streams"`
}

// FFprobeStreamV1 is a video, audio or subtitle stream embedded in the
// file. Fields that do not apply to the stream type are omitted.
type FFprobeStreamV1 struct {
	Index int `json:"index"`
	// Type is "video", "audio" or "subtitle".
	Type    string `json:"type"`
	Codec   string `json:"codec"`
	Profile string `json:"profile,omitempty"`
	Level   int    `json:"level,omitempty"`
	// Language is the tag of the file (usually ISO 639-2, e.g. "eng").
	Language        string `json:"language,omitempty"`
	Title           string `json:"title,omitempty"`
	Default         bool   `json:"default,omitempty"`
	Forced          bool   `json:"forced,omitempty"`
	HearingImpaired bool   `json:"hearing_impaired,omitempty"`
	// BitRate is in bits per second.
	BitRate int64 `json:"bit_rate,omitempty"`

	// Video.
	Width          int     `json:"width,omitempty"`
	Height         int     `json:"height,omitempty"`
	AspectRatio    string  `json:"aspect_ratio,omitempty"`
	FrameRate      float64 `json:"frame_rate,omitempty"`
	PixelFormat    string  `json:"pixel_format,omitempty"`
	BitDepth       int     `json:"bit_depth,omitempty"`
	Interlaced     bool    `json:"interlaced,omitempty"`
	ColorSpace     string  `json:"color_space,omitempty"`
	ColorTransfer  string  `json:"color_transfer,omitempty"`
	ColorPrimaries string  `json:"color_primaries,omitempty"`

	// Audio.
	Channels      int    `json:"channels,omitempty"`
	ChannelLayout string `json:"channel_layout,omitempty"`
	SampleRate    int    `json:"sample_rate,omitempty"`
}
