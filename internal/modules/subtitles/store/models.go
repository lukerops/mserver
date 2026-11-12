package store

import "github.com/google/uuid"

// SubtitleFile is a subtitle file of the storage.
type SubtitleFile struct {
	FileID     uuid.UUID
	LocationID uuid.UUID
	// Path is relative to the location.
	Path string
	// Dir is the directory of Path ("." at the root of the location).
	Dir string
	// Format is the format of the file (e.g. "srt").
	Format string
}

// Item is a movie or an episode of the library, with its video file.
type Item struct {
	ID uuid.UUID
	// Type is "movie" or "episode".
	Type       string
	FileID     uuid.UUID
	LocationID uuid.UUID
	// Path is relative to the location.
	Path string
	// Dir is the directory of Path ("." at the root of the location).
	Dir string
}

// Match is a subtitle file matched with an item of the same directory.
type Match struct {
	// ID is derived from ItemID and FileID.
	ID       uuid.UUID
	ItemID   uuid.UUID
	ItemType string
	FileID   uuid.UUID
	// LocationID and Dir are the ones of the item and of the file.
	LocationID uuid.UUID
	Dir        string
	Format     string
	// Language is a BCP 47 tag, empty when unknown.
	Language        string
	Title           string
	Forced          bool
	HearingImpaired bool
	Source          string
}
