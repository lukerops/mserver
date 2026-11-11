package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/analyzer"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
)

// StableAfter is how long a file must go unmodified before it is analyzed:
// files modified more recently may still be being written (e.g. a copy or
// a download in progress).
const StableAfter = time.Minute

// AnalyzePending runs the analyzers on the files they accept that were not
// analyzed in their current state and are unmodified for StableAfter. It
// returns how many files were analyzed. It stops at the first failure that
// is not about the file itself (e.g. the analyzer is not installed).
func (s *Service) AnalyzePending(ctx context.Context) (int, error) {
	var (
		analyzed int
		drivers  = make(map[uuid.UUID]driver.Driver)
	)
	for _, a := range s.analyzers {
		files, err := s.store.PendingAnalysis(ctx, a.Name(), a.Version(), time.Now().Add(-StableAfter))
		if err != nil {
			return analyzed, err
		}
		for i := range files {
			f := &files[i]
			if !a.Accepts(f.Path) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return analyzed, err
			}
			d, ok := drivers[f.LocationID]
			if !ok {
				loc, err := s.store.GetLocation(ctx, f.LocationID)
				if errors.Is(err, ErrNotFound) {
					s.log.DebugContext(ctx, "storage: file skipped, location deleted",
						slog.String("analyzer", a.Name()),
						slog.String("file_id", f.ID.String()),
						slog.String("location_id", f.LocationID.String()),
					)
					continue
				}
				if err != nil {
					return analyzed, err
				}
				if d, err = newDriver(loc.Driver, loc.Options); err != nil {
					return analyzed, err
				}
				drivers[f.LocationID] = d
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
func (s *Service) analyze(ctx context.Context, a analyzer.Analyzer, d driver.Driver, f *File) error {
	log := s.log.With(
		slog.String("analyzer", a.Name()),
		slog.String("file_id", f.ID.String()),
		slog.String("location_id", f.LocationID.String()),
	)
	log.DebugContext(ctx, "storage: analysis started", slog.String("path", f.Path))
	start := time.Now()
	result, err := a.Analyze(ctx, d.LocalPath(f.Path))
	duration := time.Since(start)
	analysis := &Analysis{
		FileID:     f.ID,
		Analyzer:   a.Name(),
		Version:    a.Version(),
		Size:       f.Size,
		ModTime:    f.ModTime,
		AnalyzedAt: time.Now().UTC(),
	}
	switch {
	case errors.Is(err, analyzer.ErrInvalidFile):
		log.WarnContext(ctx, "storage: file cannot be analyzed",
			slog.String("path", f.Path),
			slog.Int64("duration", int64(duration)),
			slog.String("error", err.Error()),
		)
		analysis.Error = err.Error()
	case err != nil:
		return fmt.Errorf("%w: analyzer %s: file %s: %w", ErrUnavailable, a.Name(), f.ID, err)
	default:
		if analysis.Result, err = json.Marshal(result); err != nil {
			return fmt.Errorf("analyzer %s: encode result: %w", a.Name(), err)
		}
	}

	dropped, err := s.saveAnalysis(ctx, f, analysis)
	switch {
	case err != nil:
		return err
	case dropped != "":
		log.DebugContext(ctx, "storage: analysis dropped",
			slog.String("reason", dropped),
			slog.Int64("duration", int64(duration)),
		)
	default:
		log.DebugContext(ctx, "storage: analysis finished",
			slog.Bool("ok", analysis.Error == ""),
			slog.Int64("duration", int64(duration)),
		)
	}
	return nil
}

// saveAnalysis saves the analysis of f and, if it succeeded, publishes it.
// It returns the reason why the analysis was dropped instead: the file may
// have been removed or changed while it was being analyzed (a changed file
// is analyzed again on the next round).
//
// The lock is held until the event is published, so that a scan cannot
// publish the removal of the file before its analysis.
func (s *Service) saveAnalysis(ctx context.Context, f *File, analysis *Analysis) (dropped string, err error) {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	current, err := s.store.GetFile(ctx, f.ID)
	if errors.Is(err, ErrNotFound) {
		return "file removed", nil
	}
	if err != nil {
		return "", err
	}
	if current.Size != f.Size || !current.ModTime.Equal(f.ModTime) {
		return "file changed", nil
	}
	if err := s.store.SaveAnalysis(ctx, analysis); err != nil {
		return "", err
	}
	if analysis.Result != nil {
		if err := s.publishAnalysis(ctx, f.LocationID, analysis); err != nil {
			s.logPublishError(ctx, err)
		}
	}
	return "", nil
}

// RunAnalysis runs AnalyzePending every interval and after every scan that
// changes the index, until ctx is canceled.
func (s *Service) RunAnalysis(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		start := time.Now()
		n, err := s.AnalyzePending(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.WarnContext(ctx, "storage: analysis interrupted",
				slog.Int("analyzed", n),
				slog.String("error", err.Error()),
			)
		}
		if n > 0 {
			s.log.DebugContext(ctx, "storage: files analyzed",
				slog.Int("count", n),
				slog.Int64("duration", int64(time.Since(start))),
			)
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
func (s *Service) kickAnalysis() {
	select {
	case s.analysisKick <- struct{}{}:
	default:
	}
}
