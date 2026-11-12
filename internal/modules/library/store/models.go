package store

import (
	"bytes"
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Media types of the libraries.
const (
	MediaTypeMovies = "movies"
	// MediaTypeTVShows libraries hold series, cataloged as series, seasons
	// and episodes.
	MediaTypeTVShows = "tvshows"
)

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
	// ExternalSubtitles are the subtitle files beside the video, in a
	// stable order (see SortSubtitles).
	ExternalSubtitles []ExternalSubtitle
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ExternalSubtitle is a subtitle file beside the video file of a movie or
// episode, as matched by the subtitles module. It is saved as JSON.
type ExternalSubtitle struct {
	// ID identifies the subtitle of the item, as set by the subtitles
	// module.
	ID uuid.UUID `json:"id"`
	// FileID is the subtitle file in the storage.
	FileID uuid.UUID `json:"file_id"`
	// Format is the format of the file (e.g. "srt").
	Format string `json:"format"`
	// Language is a BCP 47 tag (e.g. "pt-BR"), empty when unknown.
	Language        string `json:"language,omitempty"`
	Title           string `json:"title,omitempty"`
	Forced          bool   `json:"forced,omitempty"`
	HearingImpaired bool   `json:"hearing_impaired,omitempty"`
	// Source tells where the subtitle came from (e.g. "local").
	Source string `json:"source"`
}

// SortSubtitles orders subtitles by language, title, forced flag and ID, so
// that the list of an item does not depend on the order they were matched.
func SortSubtitles(subs []ExternalSubtitle) {
	slices.SortFunc(subs, func(a, b ExternalSubtitle) int {
		return cmp.Or(
			strings.Compare(a.Language, b.Language),
			strings.Compare(a.Title, b.Title),
			boolCompare(a.Forced, b.Forced),
			bytes.Compare(a.ID[:], b.ID[:]),
		)
	})
}

func boolCompare(a, b bool) int {
	switch {
	case a == b:
		return 0
	case b:
		return -1
	}
	return 1
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

// Equal reports whether m and o hold the same information; nil is only
// equal to nil.
func (m *MediaInfo) Equal(o *MediaInfo) bool {
	if m == nil || o == nil {
		return m == o
	}
	return m.Container == o.Container && m.DurationMs == o.DurationMs &&
		m.BitRate == o.BitRate && m.Size == o.Size && slices.Equal(m.Streams, o.Streams)
}

// Sort orders of ListMovies and ListSeries.
const (
	SortByTitle = "title"
	SortByAdded = "added"
	SortByYear  = "year"
	// SortByNumber sorts episodes by series, season and episode number.
	SortByNumber = "number"
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

// Series groups the seasons of a show. It is a logical entity: it has no
// folder, and its episodes may come from several folders and locations of
// its library. Episodes join the series of their library with the same
// key and year.
type Series struct {
	// ID is a UUIDv7 given when the series is created.
	ID        uuid.UUID
	LibraryID uuid.UUID
	Title     string
	SortTitle string
	// Key is the normalized title the episodes are matched by.
	Key string
	// Year is zero when unknown.
	Year      int
	CreatedAt time.Time
	// SeasonCount and EpisodeCount are filled by the reads of the store and
	// ignored by the writes.
	SeasonCount  int
	EpisodeCount int
}

// Season groups the episodes of a series with the same season number. It
// is removed with its last episode.
type Season struct {
	// ID is a UUIDv7 given when the season is created.
	ID        uuid.UUID
	SeriesID  uuid.UUID
	LibraryID uuid.UUID
	// Number is 0 for the specials.
	Number    int
	CreatedAt time.Time
	// SeriesTitle, SeriesYear and EpisodeCount are filled by the reads of
	// the store and ignored by the writes.
	SeriesTitle  string
	SeriesYear   int
	EpisodeCount int
}

// Episode is an episode identified from a file of the storage. A file may
// hold several episodes (Number to EndNumber).
type Episode struct {
	// ID is derived from FileID.
	ID         uuid.UUID
	LibraryID  uuid.UUID
	SeriesID   uuid.UUID
	SeasonID   uuid.UUID
	FileID     uuid.UUID
	LocationID uuid.UUID
	// Path is relative to the location.
	Path  string
	Title string
	// SortKey sorts the episodes of a season without number naturally
	// ("ep2" before "ep10").
	SortKey string
	// Number is nil when unknown. EndNumber is the last episode of a file
	// holding several episodes, or nil.
	Number    *int
	EndNumber *int
	// MediaInfo is nil until the file is analyzed.
	MediaInfo *MediaInfo
	// ExternalSubtitles are the subtitle files beside the video, in a
	// stable order (see SortSubtitles).
	ExternalSubtitles []ExternalSubtitle
	CreatedAt         time.Time
	UpdatedAt         time.Time
	// SeriesTitle, SeriesYear and SeasonNumber are filled by the reads of
	// the store and ignored by the writes.
	SeriesTitle  string
	SeriesYear   int
	SeasonNumber int
}

// SeriesQuery selects a page of series.
type SeriesQuery struct {
	// LibraryID restricts the series to a library, unless it is uuid.Nil.
	LibraryID uuid.UUID
	// IDs restricts the series to these IDs, unless it is empty.
	IDs []uuid.UUID
	// Years restricts the series to these years, unless it is empty.
	Years []int
	// AudioLanguages and SubtitleLanguages restrict the series to the ones
	// with an episode with an audio (subtitle) stream in one of these
	// languages, unless they are empty.
	AudioLanguages    []string
	SubtitleLanguages []string
	// SortBy is SortByTitle (default), SortByAdded or SortByYear.
	SortBy     string
	Descending bool
	Offset     int
	// Limit is the maximum number of series; zero means no limit.
	Limit int
}

// SeasonQuery selects seasons, ordered by series and number.
type SeasonQuery struct {
	// LibraryID, SeriesID and IDs restrict the seasons, unless they are
	// zero.
	LibraryID uuid.UUID
	SeriesID  uuid.UUID
	IDs       []uuid.UUID
}

// EpisodeQuery selects a page of episodes.
type EpisodeQuery struct {
	// LibraryID, SeriesID, SeasonID and IDs restrict the episodes, unless
	// they are zero.
	LibraryID uuid.UUID
	SeriesID  uuid.UUID
	SeasonID  uuid.UUID
	IDs       []uuid.UUID
	// AudioLanguages and SubtitleLanguages restrict the episodes as in
	// MovieQuery.
	AudioLanguages    []string
	SubtitleLanguages []string
	// SortBy is SortByNumber (default: series title, season number,
	// episode number, then naturally by file name; episodes without number
	// last) or SortByAdded.
	SortBy     string
	Descending bool
	Offset     int
	// Limit is the maximum number of episodes; zero means no limit.
	Limit int
}

// ShowChanges are the series and seasons that SaveEpisode and DeleteEpisode
// created or removed along with the episode. The fields are nil when
// nothing happened to them.
type ShowChanges struct {
	AddedSeries   *Series
	AddedSeason   *Season
	RemovedSeason *Season
	RemovedSeries *Series
}
