package events

import "github.com/google/uuid"

// SourceSubtitles is the CloudEvents source of the subtitles events.
const SourceSubtitles = "subtitles"

// Types of the subtitle events. The subject is the subtitle id.
const (
	// SubtitleAddedV1 is a subtitle matched with an item, or a matched
	// subtitle whose attributes changed (consumers upsert it by id). Its
	// data is SubtitleEventV1.
	SubtitleAddedV1 = "subtitles.subtitle.added.v1"
	// SubtitleRemovedV1 is a subtitle no longer matched with its item
	// (because the file or the item was removed, moved or renamed). Its
	// data is SubtitleRemovedEventV1.
	SubtitleRemovedV1 = "subtitles.subtitle.removed.v1"
)

// Types of the items a subtitle belongs to (SubtitleEventV1.ItemType).
const (
	ItemTypeMovie   = "movie"
	ItemTypeEpisode = "episode"
)

// Origins of a subtitle (SubtitleEventV1.Source).
const (
	// SubtitleSourceLocal is a subtitle file found beside the video.
	SubtitleSourceLocal = "local"
)

// SubtitleEventV1 is the data of the subtitle added event.
type SubtitleEventV1 struct {
	// SubtitleID is derived from the item and the subtitle file.
	SubtitleID uuid.UUID `json:"subtitle_id"`
	// ItemID is the id of the movie or episode, in the library.
	ItemID uuid.UUID `json:"item_id"`
	// ItemType is ItemTypeMovie or ItemTypeEpisode.
	ItemType string `json:"item_type"`
	// FileID is the subtitle file in the storage.
	FileID uuid.UUID `json:"file_id"`
	// Format is the format of the file (e.g. "srt").
	Format string `json:"format"`
	// Language is a BCP 47 tag (e.g. "pt-BR"), omitted when unknown.
	Language          string `json:"language,omitempty"`
	Title             string `json:"title,omitempty"`
	IsForced          bool   `json:"is_forced,omitempty"`
	IsHearingImpaired bool   `json:"is_hearing_impaired,omitempty"`
	// Source is SubtitleSourceLocal.
	Source string `json:"source"`
}

// SubtitleRemovedEventV1 is the data of the subtitle removed event.
type SubtitleRemovedEventV1 struct {
	SubtitleID uuid.UUID `json:"subtitle_id"`
	ItemID     uuid.UUID `json:"item_id"`
	FileID     uuid.UUID `json:"file_id"`
}
