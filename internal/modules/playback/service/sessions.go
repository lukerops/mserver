package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/kv"
)

func key(id uuid.UUID) string { return keyPrefix + id.String() }

// Start creates a session, with an id of its own.
func (s *Service) Start(ctx context.Context, req *StartRequest) (*Session, error) {
	for _, f := range []struct {
		name string
		id   uuid.UUID
	}{
		{"account_id", req.AccountID},
		{"profile_id", req.ProfileID},
		{"device_id", req.DeviceID},
		{"item_id", req.ItemID},
		{"file_id", req.FileID},
	} {
		if f.id == uuid.Nil {
			return nil, fmt.Errorf("%w: missing %s", ErrInvalidArgument, f.name)
		}
	}
	if req.PlayMethod != events.PlayMethodDirectPlay {
		return nil, fmt.Errorf("%w: unsupported play_method %q", ErrInvalidArgument, req.PlayMethod)
	}
	if req.PositionMs < 0 {
		return nil, fmt.Errorf("%w: negative position_ms", ErrInvalidArgument)
	}

	now := s.now().UTC()
	sess := &Session{
		ID:         uuid.Must(uuid.NewV7()),
		AccountID:  req.AccountID,
		ProfileID:  req.ProfileID,
		DeviceID:   req.DeviceID,
		ItemID:     req.ItemID,
		FileID:     req.FileID,
		PlayMethod: req.PlayMethod,
		PositionMs: req.PositionMs,
		Paused:     req.Paused,
		Sequence:   1,
		StartedAt:  now,
		UpdatedAt:  now,
		LastSeenAt: now,
	}
	value, err := json.Marshal(sess)
	if err != nil {
		return nil, fmt.Errorf("playback: encode session: %w", err)
	}
	if err := s.kv.Create(ctx, key(sess.ID), value, s.ttl()); err != nil {
		return nil, fmt.Errorf("playback: save session: %w", err)
	}
	s.publish(ctx, startedEvent(sess))
	s.log.InfoContext(ctx, "playback: session started",
		slog.String("playback_session_id", sess.ID.String()),
		slog.String("account_id", sess.AccountID.String()),
		slog.String("profile_id", sess.ProfileID.String()),
		slog.String("device_id", sess.DeviceID.String()),
		slog.String("item_id", sess.ItemID.String()),
		slog.String("file_id", sess.FileID.String()),
		slog.String("play_method", sess.PlayMethod),
		slog.Int64("position_ms", sess.PositionMs),
		slog.Bool("is_paused", sess.Paused),
	)
	return sess, nil
}

// warnNotFound logs a report (op) of a client for a session that does not
// exist, or no longer does (stopped or expired).
func (s *Service) warnNotFound(ctx context.Context, id uuid.UUID, op string, err error) {
	if errors.Is(err, ErrNotFound) {
		s.log.WarnContext(ctx, "playback: report for unknown session",
			slog.String("playback_session_id", id.String()),
			slog.String("op", op),
		)
	}
}

// Get returns an active session, or ErrNotFound.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Session, error) {
	value, err := s.kv.Get(ctx, key(id))
	if errors.Is(err, kv.ErrNotFound) {
		return nil, fmt.Errorf("%w: playback session %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("playback: get session: %w", err)
	}
	var sess Session
	if err := json.Unmarshal(value, &sess); err != nil {
		return nil, fmt.Errorf("playback: decode session %s: %w", id, err)
	}
	return &sess, nil
}

// List lists the active sessions, oldest first. A limit of zero or less
// lists them all.
func (s *Service) List(ctx context.Context, limit, offset int) ([]Session, error) {
	sessions, err := s.list(ctx)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		sessions = sessions[min(offset, len(sessions)):]
	}
	if limit > 0 && limit < len(sessions) {
		sessions = sessions[:limit]
	}
	return sessions, nil
}

// list returns every active session, oldest first.
func (s *Service) list(ctx context.Context) ([]Session, error) {
	entries, err := s.kv.List(ctx, keyPrefix)
	if err != nil {
		return nil, fmt.Errorf("playback: list sessions: %w", err)
	}
	sessions := make([]Session, 0, len(entries))
	for i := range entries {
		var sess Session
		if err := json.Unmarshal(entries[i].Value, &sess); err != nil {
			s.log.ErrorContext(ctx, "playback: decode session",
				slog.String("key", entries[i].Key),
				slog.String("error", err.Error()),
			)
			continue
		}
		sessions = append(sessions, sess)
	}
	slices.SortFunc(sessions, func(a, b Session) int {
		if c := a.StartedAt.Compare(b.StartedAt); c != 0 {
			return c
		}
		return slices.Compare(a.ID[:], b.ID[:])
	})
	return sessions, nil
}

// touch accounts the time since the last report and marks the session as
// seen now.
func touch(sess *Session, now time.Time) {
	if !sess.Paused && now.After(sess.LastSeenAt) {
		sess.WatchedMs += now.Sub(sess.LastSeenAt).Milliseconds()
	}
	sess.LastSeenAt = now
}

// update saves a changed session. A session that is gone (stopped or
// expired meanwhile) is ErrNotFound.
func (s *Service) update(ctx context.Context, sess *Session) error {
	value, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("playback: encode session: %w", err)
	}
	err = s.kv.Update(ctx, key(sess.ID), value, s.ttl())
	if errors.Is(err, kv.ErrNotFound) {
		return fmt.Errorf("%w: playback session %s", ErrNotFound, sess.ID)
	}
	if err != nil {
		return fmt.Errorf("playback: save session: %w", err)
	}
	return nil
}

// Progress reports the position and the pause state of a session. It
// publishes the progressed event, and the paused or resumed one when the
// pause state changes.
func (s *Service) Progress(ctx context.Context, id uuid.UUID, req *ProgressRequest) (*Session, error) {
	if req.PositionMs < 0 {
		return nil, fmt.Errorf("%w: negative position_ms", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.Get(ctx, id)
	if err != nil {
		s.warnNotFound(ctx, id, "progress", err)
		return nil, err
	}

	now := s.now().UTC()
	touch(sess, now)
	wasPaused := sess.Paused
	sess.PositionMs = req.PositionMs
	sess.Paused = req.Paused
	sess.UpdatedAt = now

	// The progressed event has the sequence before the paused or resumed
	// event of the same report, if any.
	sess.Sequence++
	evs := []event{progressedEvent(sess, now)}
	if sess.Paused != wasPaused {
		sess.Sequence++
		if sess.Paused {
			evs = append(evs, pausedEvent(sess, now))
		} else {
			evs = append(evs, resumedEvent(sess, now))
		}
	}
	if err := s.update(ctx, sess); err != nil {
		s.warnNotFound(ctx, id, "progress", err)
		return nil, err
	}
	s.publish(ctx, evs...)
	s.log.DebugContext(ctx, "playback: progress reported",
		slog.String("playback_session_id", sess.ID.String()),
		slog.Int64("position_ms", sess.PositionMs),
		slog.Bool("is_paused", sess.Paused),
		slog.Int64("sequence", sess.Sequence),
	)
	if sess.Paused != wasPaused {
		msg := "playback: session resumed"
		if sess.Paused {
			msg = "playback: session paused"
		}
		s.log.DebugContext(ctx, msg,
			slog.String("playback_session_id", sess.ID.String()),
			slog.Int64("position_ms", sess.PositionMs),
		)
	}
	return sess, nil
}

// Heartbeat keeps a session alive without changing it.
func (s *Service) Heartbeat(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.Get(ctx, id)
	if err != nil {
		s.warnNotFound(ctx, id, "heartbeat", err)
		return err
	}
	touch(sess, s.now().UTC())
	if err := s.update(ctx, sess); err != nil {
		s.warnNotFound(ctx, id, "heartbeat", err)
		return err
	}
	s.log.DebugContext(ctx, "playback: heartbeat",
		slog.String("playback_session_id", sess.ID.String()),
		slog.Int64("watched_duration_ms", sess.WatchedMs),
	)
	return nil
}

// Stop ends a session.
func (s *Service) Stop(ctx context.Context, id uuid.UUID, req *StopRequest) error {
	if req.PositionMs != nil && *req.PositionMs < 0 {
		return fmt.Errorf("%w: negative position_ms", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.Get(ctx, id)
	if err != nil {
		s.warnNotFound(ctx, id, "stop", err)
		return err
	}
	now := s.now().UTC()
	touch(sess, now)
	if req.PositionMs != nil {
		sess.PositionMs = *req.PositionMs
	}
	if err := s.end(ctx, sess, now, events.StopReasonUser); err != nil {
		s.warnNotFound(ctx, id, "stop", err)
		return err
	}
	return nil
}

// end deletes a session and publishes its stopped event. Of the instances
// ending the same session, only the one that deletes it publishes the
// event; the others get ErrNotFound.
func (s *Service) end(ctx context.Context, sess *Session, now time.Time, reason string) error {
	err := s.kv.Delete(ctx, key(sess.ID))
	if errors.Is(err, kv.ErrNotFound) {
		return fmt.Errorf("%w: playback session %s", ErrNotFound, sess.ID)
	}
	if err != nil {
		return fmt.Errorf("playback: delete session: %w", err)
	}
	sess.Sequence++
	s.publish(ctx, stoppedEvent(sess, now, reason))
	s.log.InfoContext(ctx, "playback: session stopped",
		slog.String("playback_session_id", sess.ID.String()),
		slog.String("profile_id", sess.ProfileID.String()),
		slog.String("device_id", sess.DeviceID.String()),
		slog.String("item_id", sess.ItemID.String()),
		slog.String("file_id", sess.FileID.String()),
		slog.String("reason", reason),
		slog.Int64("position_ms", sess.PositionMs),
		slog.Int64("watched_duration_ms", sess.WatchedMs),
	)
	return nil
}
