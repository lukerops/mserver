package service

import (
	"context"
	"log/slog"
)

// resyncPageSize is how many matches ResyncSubtitles reads at a time.
const resyncPageSize = 1000

// ResyncSubtitles publishes the added event of every matched subtitle. It
// stops at the first failure.
func (s *Service) ResyncSubtitles(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for offset := 0; ; offset += resyncPageSize {
		page, err := s.store.ListMatches(ctx, resyncPageSize, offset)
		if err != nil {
			return err
		}
		evs := make([]event, len(page))
		for i := range page {
			evs[i] = subtitleAddedEvent(&page[i])
		}
		if err := s.publish(ctx, evs...); err != nil {
			return err
		}
		count += len(page)
		if len(page) < resyncPageSize {
			break
		}
	}
	s.log.DebugContext(ctx, "subtitles: subtitles resynced", slog.Int("count", count))
	return nil
}
