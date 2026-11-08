package router

import (
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/library/service"
)

// ErrorV1 is the body of every error response.
type ErrorV1 struct {
	Error string `json:"error"`
}

type LibraryV1 struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// MediaType is "movies".
	MediaType   string      `json:"media_type"`
	LocationIDs []uuid.UUID `json:"location_ids"`
	CreatedAt   time.Time   `json:"created_at"`
}

type LibraryListV1 struct {
	Libraries []LibraryV1 `json:"libraries"`
}

type CreateLibraryRequestV1 struct {
	Name string `json:"name"`
	// MediaType must be "movies".
	MediaType string `json:"media_type"`
	// LocationIDs are storage locations that belong to no other library.
	LocationIDs []uuid.UUID `json:"location_ids"`
}

type MovieV1 struct {
	ID         uuid.UUID `json:"id"`
	LibraryID  uuid.UUID `json:"library_id"`
	FileID     uuid.UUID `json:"file_id"`
	LocationID uuid.UUID `json:"location_id"`
	// Path is relative to the location.
	Path      string `json:"path"`
	Title     string `json:"title"`
	SortTitle string `json:"sort_title"`
	// Year is omitted when unknown.
	Year int `json:"year,omitempty"`
	// MediaInfo is omitted until the file is analyzed.
	MediaInfo *MediaInfoV1 `json:"media_info,omitempty"`
	// CreatedAt is when the movie was added to the library.
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MediaInfoV1 is the technical information of the file of a movie.
type MediaInfoV1 struct {
	// Container is the format name (e.g. "matroska,webm").
	Container  string `json:"container"`
	DurationMs int64  `json:"duration_ms"`
	// BitRate is in bits per second.
	BitRate int64      `json:"bit_rate,omitempty"`
	Size    int64      `json:"size,omitempty"`
	Streams []StreamV1 `json:"streams"`
}

// StreamV1 is a video, audio or subtitle stream embedded in a file.
type StreamV1 struct {
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

// MovieListV1 is a page of movies.
type MovieListV1 struct {
	Movies []MovieV1 `json:"movies"`
	// Total is the number of movies matching the query, ignoring the
	// offset and limit.
	Total int `json:"total"`
}

// MovieQueryV1 holds the query parameters of GET /v1/movies:
//
//	library_id          only the movies of this library
//	ids                 only these movies (comma-separated)
//	years               only the movies of these years (comma-separated)
//	audio_languages     only the movies with an audio stream in one of these
//	                    languages (comma-separated, case-insensitive)
//	subtitle_languages  likewise, with a subtitle stream
//	sort_by             title (default), added or year
//	order               asc (default) or desc
//	offset              movies to skip
//	limit               maximum number of movies (default: all)
type MovieQueryV1 struct {
	LibraryID         uuid.UUID
	IDs               []uuid.UUID
	Years             []int
	AudioLanguages    []string
	SubtitleLanguages []string
	SortBy            string
	Descending        bool
	Offset            int
	Limit             int
}

func toLibraryV1(l service.Library) LibraryV1 {
	locations := l.LocationIDs
	if locations == nil {
		locations = []uuid.UUID{}
	}
	return LibraryV1{
		ID:          l.ID,
		Name:        l.Name,
		MediaType:   l.MediaType,
		LocationIDs: locations,
		CreatedAt:   l.CreatedAt,
	}
}

func toMovieV1(m service.Movie) MovieV1 {
	v := MovieV1{
		ID:         m.ID,
		LibraryID:  m.LibraryID,
		FileID:     m.FileID,
		LocationID: m.LocationID,
		Path:       m.Path,
		Title:      m.Title,
		SortTitle:  m.SortTitle,
		Year:       m.Year,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
	if info := m.MediaInfo; info != nil {
		v.MediaInfo = &MediaInfoV1{
			Container:  info.Container,
			DurationMs: info.DurationMs,
			BitRate:    info.BitRate,
			Size:       info.Size,
			Streams:    make([]StreamV1, len(info.Streams)),
		}
		for i, s := range info.Streams {
			v.MediaInfo.Streams[i] = StreamV1(s)
		}
	}
	return v
}
