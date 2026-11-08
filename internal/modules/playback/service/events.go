package service

import (
	"time"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/playback/events"
)

func startedEvent(s Session) queue.Envelope[events.SessionStartedEventV1] {
	return queue.NewEnvelope(events.SessionStartedV1, events.Source, s.ID.String(), events.SessionStartedEventV1{
		PlaybackSessionID: s.ID,
		AccountID:         s.AccountID,
		ProfileID:         s.ProfileID,
		DeviceID:          s.DeviceID,
		ItemID:            s.ItemID,
		FileID:            s.FileID,
		PlayMethod:        s.PlayMethod,
		PositionMs:        s.PositionMs,
		Sequence:          s.Sequence,
		StartedAt:         s.StartedAt,
	})
}

// progressedEvent has the sequence before the paused or resumed event
// of the same report, if any.
func progressedEvent(s Session, at time.Time) queue.Envelope[events.SessionProgressedEventV1] {
	return queue.NewEnvelope(events.SessionProgressedV1, events.Source, s.ID.String(), events.SessionProgressedEventV1{
		PlaybackSessionID: s.ID,
		ProfileID:         s.ProfileID,
		DeviceID:          s.DeviceID,
		ItemID:            s.ItemID,
		PositionMs:        s.PositionMs,
		IsPaused:          s.Paused,
		Sequence:          s.Sequence,
		ReportedAt:        at,
	})
}

func pausedEvent(s Session, at time.Time) queue.Envelope[events.SessionPausedEventV1] {
	return queue.NewEnvelope(events.SessionPausedV1, events.Source, s.ID.String(), events.SessionPausedEventV1{
		PlaybackSessionID: s.ID,
		ProfileID:         s.ProfileID,
		ItemID:            s.ItemID,
		PositionMs:        s.PositionMs,
		Sequence:          s.Sequence,
		PausedAt:          at,
	})
}

func resumedEvent(s Session, at time.Time) queue.Envelope[events.SessionResumedEventV1] {
	return queue.NewEnvelope(events.SessionResumedV1, events.Source, s.ID.String(), events.SessionResumedEventV1{
		PlaybackSessionID: s.ID,
		ProfileID:         s.ProfileID,
		ItemID:            s.ItemID,
		PositionMs:        s.PositionMs,
		Sequence:          s.Sequence,
		ResumedAt:         at,
	})
}

func stoppedEvent(s Session, at time.Time, reason string) queue.Envelope[events.SessionStoppedEventV1] {
	return queue.NewEnvelope(events.SessionStoppedV1, events.Source, s.ID.String(), events.SessionStoppedEventV1{
		PlaybackSessionID: s.ID,
		AccountID:         s.AccountID,
		ProfileID:         s.ProfileID,
		DeviceID:          s.DeviceID,
		ItemID:            s.ItemID,
		PositionMs:        s.PositionMs,
		WatchedDurationMs: s.WatchedMs,
		Sequence:          s.Sequence,
		StoppedAt:         at,
		Reason:            reason,
	})
}
