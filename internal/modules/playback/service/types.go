package service

import (
	"time"

	"github.com/google/uuid"
)

// Session is an active playback session.
type Session struct {
	ID         uuid.UUID `json:"id"`
	AccountID  uuid.UUID `json:"account_id"`
	ProfileID  uuid.UUID `json:"profile_id"`
	DeviceID   uuid.UUID `json:"device_id"`
	ItemID     uuid.UUID `json:"item_id"`
	FileID     uuid.UUID `json:"file_id"`
	PlayMethod string    `json:"play_method"`
	PositionMs int64     `json:"position_ms"`
	Paused     bool      `json:"paused"`
	// WatchedMs is the wall-clock time the session played (not paused),
	// up to LastSeenAt.
	WatchedMs int64 `json:"watched_ms"`
	// Sequence is the sequence of the last event of the session.
	Sequence   int64     `json:"sequence"`
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// StartRequest starts a session.
type StartRequest struct {
	AccountID, ProfileID, DeviceID, ItemID, FileID uuid.UUID
	// PlayMethod must be events.PlayMethodDirectPlay.
	PlayMethod string
	PositionMs int64
	Paused     bool
}

// ProgressRequest reports the state of a session.
type ProgressRequest struct {
	PositionMs int64
	Paused     bool
}

// StopRequest stops a session.
type StopRequest struct {
	// PositionMs is the final position; nil keeps the last reported one.
	PositionMs *int64
}
