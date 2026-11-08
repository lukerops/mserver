package store

import (
	"time"

	"github.com/google/uuid"
)

// MediaTypeMovies is the media type of the movie libraries, the only one
// supported for now.
const MediaTypeMovies = "movies"

// Library groups locations of the storage holding one type of media.
type Library struct {
	ID        uuid.UUID
	Name      string
	MediaType string
	// LocationIDs are the storage locations of the library, in the order
	// they were given.
	LocationIDs []uuid.UUID
	CreatedAt   time.Time
}

// Movie is a movie identified from a file of the storage.
type Movie struct {
	// ID is derived from FileID.
	ID         uuid.UUID
	LibraryID  uuid.UUID
	FileID     uuid.UUID
	LocationID uuid.UUID
	// Path is relative to the location.
	Path      string
	Title     string
	SortTitle string
	// Year is zero when unknown.
	Year int
	// MediaInfo is nil until the file is analyzed.
	MediaInfo *MediaInfo
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MediaInfo is the technical information of a media file. It is saved as
// JSON.
type MediaInfo struct {
	// Container is the format name (e.g. "matroska,webm").
	Container  string `json:"container"`
	DurationMs int64  `json:"duration_ms"`
	// BitRate is in bits per second.
	BitRate int64    `json:"bit_rate,omitempty"`
	Size    int64    `json:"size,omitempty"`
	Streams []Stream `json:"streams"`
}

// Stream is a video, audio or subtitle stream embedded in a media file.
type Stream struct {
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
	BitRate         int64  `json:"bit_rate,omitempty"`

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

	Channels      int    `json:"channels,omitempty"`
	ChannelLayout string `json:"channel_layout,omitempty"`
	SampleRate    int    `json:"sample_rate,omitempty"`
}

// Sort orders of ListMovies.
const (
	SortByTitle = "title"
	SortByAdded = "added"
	SortByYear  = "year"
)

// MovieQuery selects a page of movies.
type MovieQuery struct {
	// LibraryID restricts the movies to a library, unless it is uuid.Nil.
	LibraryID uuid.UUID
	// IDs restricts the movies to these IDs, unless it is empty.
	IDs []uuid.UUID
	// Years restricts the movies to these years, unless it is empty.
	Years []int
	// AudioLanguages and SubtitleLanguages restrict the movies to the ones
	// with an audio (subtitle) stream in one of these languages, unless
	// they are empty. Languages match case-insensitively.
	AudioLanguages    []string
	SubtitleLanguages []string
	// SortBy is SortByTitle (default), SortByAdded or SortByYear. Ties
	// are broken by title and then by ID.
	SortBy     string
	Descending bool
	Offset     int
	// Limit is the maximum number of movies; zero means no limit.
	Limit int
}
