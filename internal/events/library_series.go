package events

import "github.com/google/uuid"

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
