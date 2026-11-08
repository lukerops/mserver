package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lukerops/mserver/internal/modules/storage/analyzer"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
)

// StableAfter is how long a file must go unmodified before it is analyzed:
// files modified more recently may still be being written (e.g. a copy or
// a download in progress).
const StableAfter = time.Minute

func (s *service) AnalyzePending(ctx context.Context) (int, error) {
	var (
		analyzed int
		drivers  = make(map[string]driver.Driver)
	)
	for _, a := range s.analyzers {
		files, err := s.store.PendingAnalysis(ctx, a.Name(), a.Version(), time.Now().Add(-StableAfter))
		if err != nil {
			return analyzed, err
		}
		for _, f := range files {
			if !a.Accepts(f.Path) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return analyzed, err
			}
			d, ok := drivers[f.LocationID.String()]
			if !ok {
				loc, err := s.store.GetLocation(ctx, f.LocationID)
				if errors.Is(err, ErrNotFound) {
					continue // deleted meanwhile
				}
				if err != nil {
					return analyzed, err
				}
				if d, err = newDriver(loc.Driver, loc.Options); err != nil {
					return analyzed, err
				}
				drivers[f.LocationID.String()] = d
			}
			// A failure that is not about the file (e.g. ffprobe is not
			// installed) would fail for every file: stop and retry later.
			if err := s.analyze(ctx, a, d, f); err != nil {
				return analyzed, err
			}
			analyzed++
		}
	}
	return analyzed, nil
}

// analyze runs an analyzer on a file and saves the result, publishing it
// if the analysis succeeded. Files that cannot be analyzed are recorded
// so that they are retried only when they change.
func (s *service) analyze(ctx context.Context, a analyzer.Analyzer, d driver.Driver, f File) error {
	result, err := a.Analyze(ctx, d.LocalPath(f.Path))
	analysis := Analysis{
		FileID:     f.ID,
		Analyzer:   a.Name(),
		Version:    a.Version(),
		Size:       f.Size,
		ModTime:    f.ModTime,
		AnalyzedAt: time.Now().UTC(),
	}
	switch {
	case errors.Is(err, analyzer.ErrInvalidFile):
		s.log.Warn("storage: file cannot be analyzed",
			"analyzer", a.Name(), "file", f.ID, "path", f.Path, "error", err)
		analysis.Error = err.Error()
	case err != nil:
		return fmt.Errorf("%w: analyzer %s: %w", ErrUnavailable, a.Name(), err)
	default:
		if analysis.Result, err = json.Marshal(result); err != nil {
			return fmt.Errorf("analyzer %s: encode result: %w", a.Name(), err)
		}
	}

	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	return s.store.WithTx(ctx, func(ctx context.Context) error {
		// The file may have been removed or changed while it was being
		// analyzed; a changed file is analyzed again on the next round.
		current, err := s.store.GetFile(ctx, f.ID)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.Size != f.Size || !current.ModTime.Equal(f.ModTime) {
			return nil
		}
		if err := s.store.SaveAnalysis(ctx, analysis); err != nil {
			return err
		}
		if analysis.Result == nil {
			return nil
		}
		return s.outbox.Publish(ctx, fileAnalyzedEvent(f.LocationID, analysis))
	})
}

func (s *service) RunAnalysis(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		n, err := s.AnalyzePending(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Warn("storage: analysis interrupted", "error", err)
		}
		if n > 0 {
			s.log.Debug("storage: files analyzed", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.analysisKick:
		}
	}
}

// kickAnalysis wakes up RunAnalysis, if it is waiting.
func (s *service) kickAnalysis() {
	select {
	case s.analysisKick <- struct{}{}:
	default:
	}
}
