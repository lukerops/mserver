// Package service holds the business rules of the playback module: the
// playback sessions, from start to stop, and their events.
//
// The sessions live in a kv.Store shared by every instance of the module
// (key "sessions/{id}", JSON value), so any instance can serve any
// session. A session ends when the client stops it or when it receives
// no report for the session timeout; the reaper of every instance looks
// for idle sessions and the one that deletes the key publishes the
// stopped event. The key also gets a TTL of twice the timeout, renewed on
// every write, so that sessions never leak if no reaper runs.
//
// Writes are not transactional: concurrent reports of the same session
// race and the last write wins.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/kv"
	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/playback/events"
)

// keyPrefix is the prefix of the session keys in the store.
const keyPrefix = "sessions/"

// Service is the playback business logic.
type Service interface {
	// Start creates a session, with an id of its own.
	Start(ctx context.Context, req StartRequest) (Session, error)
	Get(ctx context.Context, id uuid.UUID) (Session, error)
	// List lists the active sessions, oldest first.
	List(ctx context.Context) ([]Session, error)
	// Progress reports the position and the pause state of a session.
	Progress(ctx context.Context, id uuid.UUID, req ProgressRequest) (Session, error)
	// Heartbeat keeps a session alive without changing it.
	Heartbeat(ctx context.Context, id uuid.UUID) error
	// Stop ends a session.
	Stop(ctx context.Context, id uuid.UUID, req StopRequest) error

	// ExpireIdle stops the sessions without reports for the timeout. It
	// returns how many it stopped.
	ExpireIdle(ctx context.Context) (int, error)
	// RunReaper runs ExpireIdle every interval until ctx is canceled.
	RunReaper(ctx context.Context, interval time.Duration)
}

// Errors of the service. They are wrapped with context: use errors.Is.
var (
	ErrNotFound        = errors.New("not found")
	ErrInvalidArgument = errors.New("invalid argument")
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

type service struct {
	kv      kv.Store
	outbox  *outbox.Outbox
	log     *slog.Logger
	timeout time.Duration
	now     func() time.Time

	// mu serializes the read-modify-write of the sessions in this
	// instance. Other instances may still race (last write wins).
	mu sync.Mutex
}

// New returns the service on st. Events are saved in ob. Sessions
// without reports for timeout are stopped by the reaper.
func New(st kv.Store, ob *outbox.Outbox, log *slog.Logger, timeout time.Duration) Service {
	return &service{kv: st, outbox: ob, log: log, timeout: timeout, now: time.Now}
}

func key(id uuid.UUID) string { return keyPrefix + id.String() }

// ttl is the expiration of the keys: a safety net, longer than the
// timeout, for when no reaper runs.
func (s *service) ttl() time.Duration { return 2 * s.timeout }

func (s *service) Start(ctx context.Context, req StartRequest) (Session, error) {
	for name, id := range map[string]uuid.UUID{
		"account_id": req.AccountID, "profile_id": req.ProfileID, "device_id": req.DeviceID,
		"item_id": req.ItemID, "file_id": req.FileID,
	} {
		if id == uuid.Nil {
			return Session{}, fmt.Errorf("%w: missing %s", ErrInvalidArgument, name)
		}
	}
	if req.PlayMethod != events.PlayMethodDirectPlay {
		return Session{}, fmt.Errorf("%w: unsupported play_method %q", ErrInvalidArgument, req.PlayMethod)
	}
	if req.PositionMs < 0 {
		return Session{}, fmt.Errorf("%w: negative position_ms", ErrInvalidArgument)
	}

	now := s.now().UTC()
	sess := Session{
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
		return Session{}, fmt.Errorf("playback: encode session: %w", err)
	}
	if err := s.kv.Create(ctx, key(sess.ID), value, s.ttl()); err != nil {
		return Session{}, fmt.Errorf("playback: save session: %w", err)
	}
	if err := s.outbox.Publish(ctx, startedEvent(sess)); err != nil {
		return Session{}, err
	}
	s.log.InfoContext(ctx, "playback: session started",
		"playback_session_id", sess.ID, "account_id", sess.AccountID, "profile_id", sess.ProfileID,
		"device_id", sess.DeviceID, "item_id", sess.ItemID, "file_id", sess.FileID,
		"play_method", sess.PlayMethod, "position_ms", sess.PositionMs, "is_paused", sess.Paused)
	return sess, nil
}

// warnNotFound logs a report (op) of a client for a session that does
// not exist, or no longer does (stopped or expired).
func (s *service) warnNotFound(ctx context.Context, id uuid.UUID, op string, err error) {
	if errors.Is(err, ErrNotFound) {
		s.log.WarnContext(ctx, "playback: report for unknown session",
			"playback_session_id", id, "op", op)
	}
}

func (s *service) Get(ctx context.Context, id uuid.UUID) (Session, error) {
	value, err := s.kv.Get(ctx, key(id))
	if errors.Is(err, kv.ErrNotFound) {
		return Session{}, fmt.Errorf("%w: playback session %s", ErrNotFound, id)
	}
	if err != nil {
		return Session{}, fmt.Errorf("playback: get session: %w", err)
	}
	var sess Session
	if err := json.Unmarshal(value, &sess); err != nil {
		return Session{}, fmt.Errorf("playback: decode session %s: %w", id, err)
	}
	return sess, nil
}

func (s *service) List(ctx context.Context) ([]Session, error) {
	entries, err := s.kv.List(ctx, keyPrefix)
	if err != nil {
		return nil, fmt.Errorf("playback: list sessions: %w", err)
	}
	sessions := make([]Session, 0, len(entries))
	for _, e := range entries {
		var sess Session
		if err := json.Unmarshal(e.Value, &sess); err != nil {
			s.log.ErrorContext(ctx, "playback: decode session", "key", e.Key, "error", err)
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
func (s *service) update(ctx context.Context, sess Session) error {
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

func (s *service) Progress(ctx context.Context, id uuid.UUID, req ProgressRequest) (Session, error) {
	if req.PositionMs < 0 {
		return Session{}, fmt.Errorf("%w: negative position_ms", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.Get(ctx, id)
	if err != nil {
		s.warnNotFound(ctx, id, "progress", err)
		return Session{}, err
	}

	now := s.now().UTC()
	touch(&sess, now)
	wasPaused := sess.Paused
	sess.PositionMs = req.PositionMs
	sess.Paused = req.Paused
	sess.UpdatedAt = now

	sess.Sequence++
	evs := []queue.Event{progressedEvent(sess, now)}
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
		return Session{}, err
	}
	if err := s.outbox.Publish(ctx, evs...); err != nil {
		return Session{}, err
	}
	s.log.DebugContext(ctx, "playback: progress reported",
		"playback_session_id", sess.ID, "position_ms", sess.PositionMs,
		"is_paused", sess.Paused, "sequence", sess.Sequence)
	if sess.Paused != wasPaused {
		msg := "playback: session resumed"
		if sess.Paused {
			msg = "playback: session paused"
		}
		s.log.DebugContext(ctx, msg, "playback_session_id", sess.ID, "position_ms", sess.PositionMs)
	}
	return sess, nil
}

func (s *service) Heartbeat(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.Get(ctx, id)
	if err != nil {
		s.warnNotFound(ctx, id, "heartbeat", err)
		return err
	}
	touch(&sess, s.now().UTC())
	if err := s.update(ctx, sess); err != nil {
		s.warnNotFound(ctx, id, "heartbeat", err)
		return err
	}
	s.log.DebugContext(ctx, "playback: heartbeat",
		"playback_session_id", sess.ID, "watched_duration_ms", sess.WatchedMs)
	return nil
}

func (s *service) Stop(ctx context.Context, id uuid.UUID, req StopRequest) error {
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
	touch(&sess, now)
	if req.PositionMs != nil {
		sess.PositionMs = *req.PositionMs
	}
	if err := s.end(ctx, sess, now, events.StopReasonUser); err != nil {
		s.warnNotFound(ctx, id, "stop", err)
		return err
	}
	return nil
}

// end deletes a session and publishes its stopped event. Of the
// instances ending the same session, only the one that deletes it
// publishes the event; the others get ErrNotFound.
func (s *service) end(ctx context.Context, sess Session, now time.Time, reason string) error {
	err := s.kv.Delete(ctx, key(sess.ID))
	if errors.Is(err, kv.ErrNotFound) {
		return fmt.Errorf("%w: playback session %s", ErrNotFound, sess.ID)
	}
	if err != nil {
		return fmt.Errorf("playback: delete session: %w", err)
	}
	sess.Sequence++
	if err := s.outbox.Publish(ctx, stoppedEvent(sess, now, reason)); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "playback: session stopped",
		"playback_session_id", sess.ID, "profile_id", sess.ProfileID, "device_id", sess.DeviceID,
		"item_id", sess.ItemID, "file_id", sess.FileID, "reason", reason,
		"position_ms", sess.PositionMs, "watched_duration_ms", sess.WatchedMs)
	return nil
}

func (s *service) ExpireIdle(ctx context.Context) (int, error) {
	sessions, err := s.List(ctx)
	if err != nil {
		return 0, err
	}
	var expired int
	for _, sess := range sessions {
		if !s.idle(sess) {
			continue
		}
		switch err := s.expire(ctx, sess.ID); {
		case err == nil:
			expired++
		case errors.Is(err, ErrNotFound), errors.Is(err, errNotIdle):
			// Stopped or reported meanwhile, here or by another instance.
		default:
			return expired, err
		}
	}
	s.log.DebugContext(ctx, "playback: idle sessions checked",
		"count", len(sessions), "expired", expired, "timeout", s.timeout)
	return expired, nil
}

var errNotIdle = errors.New("session not idle")

func (s *service) idle(sess Session) bool {
	return sess.LastSeenAt.Add(s.timeout).Before(s.now())
}

// expire ends an idle session, reading it again to skip the ones
// reported since the listing.
func (s *service) expire(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if !s.idle(sess) {
		return errNotIdle
	}
	return s.end(ctx, sess, s.now().UTC(), events.StopReasonTimeout)
}

func (s *service) RunReaper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.ExpireIdle(ctx)
			if err != nil && ctx.Err() == nil {
				s.log.ErrorContext(ctx, "playback: expire idle sessions", "error", err, "expired", n)
			}
		}
	}
}
