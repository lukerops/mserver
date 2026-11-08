// Package events is the event API of the library module: the types and
// the data of the events it publishes. Like the DTOs of the router
// package, every type carries the version of the event.
//
// Events are wrapped in queue.Envelope and published on the topic named
// after their type. Delivery is at-least-once: consumers must tolerate
// duplicates.
package events

import "github.com/google/uuid"

// Source is the CloudEvents source of the library events.
const Source = "library"

// Types of the movie events. The subject is the movie id.
const (
	// MovieAddedV1 is a new movie (or a movie republished by a resync).
	// Its data is MovieEventV1.
	MovieAddedV1 = "library.movie.added.v1"
	// MovieUpdatedV1 is a changed movie (e.g. renamed or analyzed). Its
	// data is MovieEventV1.
	MovieUpdatedV1 = "library.movie.updated.v1"
	// MovieRemovedV1 is a movie whose file or library was removed. Its
	// data is MovieRemovedEventV1.
	MovieRemovedV1 = "library.movie.removed.v1"
)

// MovieEventV1 is the data of the movie added and updated events.
type MovieEventV1 struct {
	MovieID    uuid.UUID `json:"movie_id"`
	LibraryID  uuid.UUID `json:"library_id"`
	FileID     uuid.UUID `json:"file_id"`
	LocationID uuid.UUID `json:"location_id"`
	// Path is relative to the location.
	Path  string `json:"path"`
	Title string `json:"title"`
	// Year is omitted when unknown.
	Year int `json:"year,omitempty"`
	// DurationMs is omitted until the file is analyzed.
	DurationMs int64 `json:"duration_ms,omitempty"`
}

// MovieRemovedEventV1 is the data of the movie removed event.
type MovieRemovedEventV1 struct {
	MovieID   uuid.UUID `json:"movie_id"`
	LibraryID uuid.UUID `json:"library_id"`
	FileID    uuid.UUID `json:"file_id"`
}

// Types of the series events. The subject is the series id. Series are
// logical: they are created with their first episode and removed with
// their last one.
const (
	// SeriesAddedV1 is a new series (or a series republished by a resync).
	// Its data is SeriesEventV1.
	SeriesAddedV1 = "library.series.added.v1"
	// SeriesUpdatedV1 is a changed series. Its data is SeriesEventV1. It is
	// not published yet: series are matched by title and year, so a series
	// with another title or year is another series.
	SeriesUpdatedV1 = "library.series.updated.v1"
	// SeriesRemovedV1 is a series left without episodes, or whose library
	// was removed. Its data is SeriesRemovedEventV1.
	SeriesRemovedV1 = "library.series.removed.v1"
)

// SeriesEventV1 is the data of the series added and updated events.
type SeriesEventV1 struct {
	SeriesID  uuid.UUID `json:"series_id"`
	LibraryID uuid.UUID `json:"library_id"`
	Title     string    `json:"title"`
	// Year is omitted when unknown.
	Year int `json:"year,omitempty"`
}

// SeriesRemovedEventV1 is the data of the series removed event.
type SeriesRemovedEventV1 struct {
	SeriesID  uuid.UUID `json:"series_id"`
	LibraryID uuid.UUID `json:"library_id"`
}

// Types of the season events. The subject is the season id. Seasons are
// created with their first episode and removed with their last one.
const (
	// SeasonAddedV1 is a new season (or a season republished by a resync).
	// Its data is SeasonEventV1.
	SeasonAddedV1 = "library.season.added.v1"
	// SeasonRemovedV1 is a season left without episodes, or whose library
	// was removed. Its data is SeasonEventV1.
	SeasonRemovedV1 = "library.season.removed.v1"
)

// SeasonEventV1 is the data of the season events.
type SeasonEventV1 struct {
	SeasonID  uuid.UUID `json:"season_id"`
	SeriesID  uuid.UUID `json:"series_id"`
	LibraryID uuid.UUID `json:"library_id"`
	// SeasonNumber is 0 for the specials.
	SeasonNumber int `json:"season_number"`
}

// Types of the episode events. The subject is the episode id. The added
// events of an episode follow the ones of its series and season; its
// removed event precedes theirs.
const (
	// EpisodeAddedV1 is a new episode (or an episode republished by a
	// resync). Its data is EpisodeEventV1.
	EpisodeAddedV1 = "library.episode.added.v1"
	// EpisodeUpdatedV1 is a changed episode (e.g. moved to another season
	// or analyzed). Its data is EpisodeEventV1.
	EpisodeUpdatedV1 = "library.episode.updated.v1"
	// EpisodeRemovedV1 is an episode whose file or library was removed.
	// Its data is EpisodeRemovedEventV1.
	EpisodeRemovedV1 = "library.episode.removed.v1"
)

// EpisodeEventV1 is the data of the episode added and updated events.
type EpisodeEventV1 struct {
	EpisodeID   uuid.UUID `json:"episode_id"`
	LibraryID   uuid.UUID `json:"library_id"`
	SeriesID    uuid.UUID `json:"series_id"`
	SeriesTitle string    `json:"series_title"`
	// SeriesYear is omitted when unknown.
	SeriesYear int       `json:"series_year,omitempty"`
	SeasonID   uuid.UUID `json:"season_id"`
	// SeasonNumber is 0 for the specials.
	SeasonNumber int `json:"season_number"`
	// EpisodeNumber is omitted when unknown. EpisodeEndNumber is the last
	// episode of a file holding several, and is omitted otherwise.
	EpisodeNumber    *int      `json:"episode_number,omitempty"`
	EpisodeEndNumber *int      `json:"episode_end_number,omitempty"`
	Title            string    `json:"title"`
	FileID           uuid.UUID `json:"file_id"`
	LocationID       uuid.UUID `json:"location_id"`
	// Path is relative to the location.
	Path string `json:"path"`
	// DurationMs is omitted until the file is analyzed.
	DurationMs int64 `json:"duration_ms,omitempty"`
}

// EpisodeRemovedEventV1 is the data of the episode removed event.
type EpisodeRemovedEventV1 struct {
	EpisodeID uuid.UUID `json:"episode_id"`
	LibraryID uuid.UUID `json:"library_id"`
	SeriesID  uuid.UUID `json:"series_id"`
	SeasonID  uuid.UUID `json:"season_id"`
	FileID    uuid.UUID `json:"file_id"`
}
