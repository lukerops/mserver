package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/pubsub"
)

// event is an event to publish: its type (the topic), subject and data.
type event struct {
	typ     string
	subject string
	data    any
}

// publish publishes the events in order, stopping at the first failure. The
// change they describe is already saved, so the publication is not canceled
// with the request, and a failure is only logged.
func (s *Service) publish(ctx context.Context, evs ...event) {
	ctx = context.WithoutCancel(ctx)
	for i := range evs {
		ev := &evs[i]
		msg, err := pubsub.Encode(pubsub.NewEnvelope(ev.typ, events.SourcePlayback, ev.subject, ev.data))
		if err == nil {
			err = s.pub.Publish(ctx, msg)
		}
		if err != nil {
			s.log.ErrorContext(ctx, "playback: publish events failed",
				slog.String("type", ev.typ),
				slog.String("subject", ev.subject),
				slog.String("error", err.Error()),
			)
			return
		}
	}
}

func startedEvent(sess *Session) event {
	return event{typ: events.SessionStartedV1, subject: sess.ID.String(), data: events.SessionStartedEventV1{
		PlaybackSessionID: sess.ID,
		AccountID:         sess.AccountID,
		ProfileID:         sess.ProfileID,
		DeviceID:          sess.DeviceID,
		ItemID:            sess.ItemID,
		FileID:            sess.FileID,
		PlayMethod:        sess.PlayMethod,
		PositionMs:        sess.PositionMs,
		Sequence:          sess.Sequence,
		StartedAt:         sess.StartedAt,
	}}
}

func progressedEvent(sess *Session, at time.Time) event {
	return event{typ: events.SessionProgressedV1, subject: sess.ID.String(), data: events.SessionProgressedEventV1{
		PlaybackSessionID: sess.ID,
		ProfileID:         sess.ProfileID,
		DeviceID:          sess.DeviceID,
		ItemID:            sess.ItemID,
		PositionMs:        sess.PositionMs,
		IsPaused:          sess.Paused,
		Sequence:          sess.Sequence,
		ReportedAt:        at,
	}}
}

func pausedEvent(sess *Session, at time.Time) event {
	return event{typ: events.SessionPausedV1, subject: sess.ID.String(), data: events.SessionPausedEventV1{
		PlaybackSessionID: sess.ID,
		ProfileID:         sess.ProfileID,
		ItemID:            sess.ItemID,
		PositionMs:        sess.PositionMs,
		Sequence:          sess.Sequence,
		PausedAt:          at,
	}}
}

func resumedEvent(sess *Session, at time.Time) event {
	return event{typ: events.SessionResumedV1, subject: sess.ID.String(), data: events.SessionResumedEventV1{
		PlaybackSessionID: sess.ID,
		ProfileID:         sess.ProfileID,
		ItemID:            sess.ItemID,
		PositionMs:        sess.PositionMs,
		Sequence:          sess.Sequence,
		ResumedAt:         at,
	}}
}

func stoppedEvent(sess *Session, at time.Time, reason string) event {
	return event{typ: events.SessionStoppedV1, subject: sess.ID.String(), data: events.SessionStoppedEventV1{
		PlaybackSessionID: sess.ID,
		AccountID:         sess.AccountID,
		ProfileID:         sess.ProfileID,
		DeviceID:          sess.DeviceID,
		ItemID:            sess.ItemID,
		PositionMs:        sess.PositionMs,
		WatchedDurationMs: sess.WatchedMs,
		Sequence:          sess.Sequence,
		StoppedAt:         at,
		Reason:            reason,
	}}
}
