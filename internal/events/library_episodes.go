package events

import "github.com/google/uuid"

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
