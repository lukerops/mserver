// Package events is the event API of the playback module: the types and
// the data of the events it publishes. Like the DTOs of the router
// package, every type carries the version of the event.
//
// Events are wrapped in queue.Envelope and published on the topic named
// after their type. Delivery is at-least-once: consumers must tolerate
// duplicates. The events of a session carry a sequence that grows by one
// on every event, to order them and detect duplicates.
package events

import (
	"time"

	"github.com/google/uuid"
)

// Source is the CloudEvents source of the playback events.
const Source = "playback"

// Types of the playback events. The subject of every event is the
// playback session id.
const (
	// SessionStartedV1 is a new playback session. The data is
	// SessionStartedEventV1.
	SessionStartedV1 = "playback.session.started.v1"
	// SessionProgressedV1 is a progress report of a session. The data is
	// SessionProgressedEventV1.
	SessionProgressedV1 = "playback.session.progressed.v1"
	// SessionPausedV1 is a session that was paused. The data is
	// SessionPausedEventV1.
	SessionPausedV1 = "playback.session.paused.v1"
	// SessionResumedV1 is a paused session that was resumed. The data is
	// SessionResumedEventV1.
	SessionResumedV1 = "playback.session.resumed.v1"
	// SessionStoppedV1 is the end of a session. The data is
	// SessionStoppedEventV1.
	SessionStoppedV1 = "playback.session.stopped.v1"
)

// Play methods.
const (
	// PlayMethodDirectPlay is the original file, served as is.
	PlayMethodDirectPlay = "direct_play"
)

// Reasons of the stopped event.
const (
	// StopReasonUser is a session stopped by the client.
	StopReasonUser = "user"
	// StopReasonTimeout is a session that received no report for the
	// session timeout.
	StopReasonTimeout = "timeout"
)

// SessionStartedEventV1 is the data of the started event.
type SessionStartedEventV1 struct {
	PlaybackSessionID uuid.UUID `json:"playback_session_id"`
	AccountID         uuid.UUID `json:"account_id"`
	ProfileID         uuid.UUID `json:"profile_id"`
	DeviceID          uuid.UUID `json:"device_id"`
	ItemID            uuid.UUID `json:"item_id"`
	FileID            uuid.UUID `json:"file_id"`
	PlayMethod        string    `json:"play_method"`
	PositionMs        int64     `json:"position_ms"`
	Sequence          int64     `json:"sequence"`
	StartedAt         time.Time `json:"started_at"`
}

// SessionProgressedEventV1 is the data of the progressed event.
type SessionProgressedEventV1 struct {
	PlaybackSessionID uuid.UUID `json:"playback_session_id"`
	ProfileID         uuid.UUID `json:"profile_id"`
	DeviceID          uuid.UUID `json:"device_id"`
	ItemID            uuid.UUID `json:"item_id"`
	PositionMs        int64     `json:"position_ms"`
	IsPaused          bool      `json:"is_paused"`
	Sequence          int64     `json:"sequence"`
	ReportedAt        time.Time `json:"reported_at"`
}

// SessionPausedEventV1 is the data of the paused event.
type SessionPausedEventV1 struct {
	PlaybackSessionID uuid.UUID `json:"playback_session_id"`
	ProfileID         uuid.UUID `json:"profile_id"`
	ItemID            uuid.UUID `json:"item_id"`
	PositionMs        int64     `json:"position_ms"`
	Sequence          int64     `json:"sequence"`
	PausedAt          time.Time `json:"paused_at"`
}

// SessionResumedEventV1 is the data of the resumed event.
type SessionResumedEventV1 struct {
	PlaybackSessionID uuid.UUID `json:"playback_session_id"`
	ProfileID         uuid.UUID `json:"profile_id"`
	ItemID            uuid.UUID `json:"item_id"`
	PositionMs        int64     `json:"position_ms"`
	Sequence          int64     `json:"sequence"`
	ResumedAt         time.Time `json:"resumed_at"`
}

// SessionStoppedEventV1 is the data of the stopped event.
type SessionStoppedEventV1 struct {
	PlaybackSessionID uuid.UUID `json:"playback_session_id"`
	AccountID         uuid.UUID `json:"account_id"`
	ProfileID         uuid.UUID `json:"profile_id"`
	DeviceID          uuid.UUID `json:"device_id"`
	ItemID            uuid.UUID `json:"item_id"`
	PositionMs        int64     `json:"position_ms"`
	// WatchedDurationMs is the wall-clock time the session played (not
	// paused), up to its last report.
	WatchedDurationMs int64     `json:"watched_duration_ms"`
	Sequence          int64     `json:"sequence"`
	StoppedAt         time.Time `json:"stopped_at"`
	// Reason is StopReasonUser or StopReasonTimeout.
	Reason string `json:"reason"`
}
