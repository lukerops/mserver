package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PendingAnalysis returns the files modified at or before stableBefore that
// have no analysis of the analyzer at this version for their current size
// and mtime, ordered by ID.
func (s *Store) PendingAnalysis(ctx context.Context, analyzer string, version int, stableBefore time.Time) ([]File, error) {
	return s.queryFiles(ctx, selectPendingAnalysis, analyzer, version, nanos(stableBefore))
}

// SaveAnalysis saves the analysis of a file, replacing the previous one of
// the same analyzer.
func (s *Store) SaveAnalysis(ctx context.Context, a *Analysis) error {
	var result any // NULL, not an empty blob, when the analysis failed
	if a.Result != nil {
		result = a.Result
	}
	_, err := s.db.ExecContext(ctx, upsertAnalysis,
		a.FileID, a.Analyzer, a.Version, a.Size, nanos(a.ModTime), result, a.Error, nanos(a.AnalyzedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save analysis of %s: %w", a.FileID, err)
	}
	return nil
}

// ListAnalyses returns the successful analyses of the files of a location
// that match the current state of the files, ordered by file ID.
func (s *Store) ListAnalyses(ctx context.Context, location uuid.UUID) ([]Analysis, error) {
	rows, err := s.db.QueryContext(ctx, selectAnalyses, location)
	if err != nil {
		return nil, fmt.Errorf("store: list analyses: %w", err)
	}
	defer rows.Close()

	var analyses []Analysis
	for rows.Next() {
		var (
			a                   Analysis
			modTime, analyzedAt int64
		)
		if err := rows.Scan(&a.FileID, &a.Analyzer, &a.Version, &a.Size, &modTime, &a.Result, &a.Error, &analyzedAt); err != nil {
			return nil, fmt.Errorf("store: list analyses: %w", err)
		}
		a.ModTime = fromNanos(modTime)
		a.AnalyzedAt = fromNanos(analyzedAt)
		analyses = append(analyses, a)
	}
	return analyses, rows.Err()
}
