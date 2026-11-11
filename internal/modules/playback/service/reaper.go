package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
)

var errNotIdle = errors.New("session not idle")

// ExpireIdle stops the sessions without reports for the timeout. It
// returns how many it stopped.
func (s *Service) ExpireIdle(ctx context.Context) (int, error) {
	sessions, err := s.list(ctx)
	if err != nil {
		return 0, err
	}
	var expired int
	for i := range sessions {
		if !s.idle(&sessions[i]) {
			continue
		}
		switch err := s.expire(ctx, sessions[i].ID); {
		case err == nil:
			expired++
		case errors.Is(err, ErrNotFound), errors.Is(err, errNotIdle):
			// Stopped or reported meanwhile, here or by another instance.
		default:
			return expired, err
		}
	}
	s.log.DebugContext(ctx, "playback: idle sessions checked",
		slog.Int("count", len(sessions)),
		slog.Int("expired", expired),
		slog.Int64("timeout", int64(s.timeout)),
	)
	return expired, nil
}

func (s *Service) idle(sess *Session) bool {
	return sess.LastSeenAt.Add(s.timeout).Before(s.now())
}

// expire ends an idle session, reading it again to skip the ones reported
// since the listing.
func (s *Service) expire(ctx context.Context, id uuid.UUID) error {
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

// RunReaper runs ExpireIdle every interval until ctx is canceled.
func (s *Service) RunReaper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.ExpireIdle(ctx)
			if err != nil && ctx.Err() == nil {
				s.log.ErrorContext(ctx, "playback: expire idle sessions",
					slog.Int("expired", n),
					slog.String("error", err.Error()),
				)
			}
		}
	}
}
