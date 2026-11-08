package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/modules/storage/store"
)

func (s *Store) PendingAnalysis(ctx context.Context, analyzer string, version int, stableBefore time.Time) ([]store.File, error) {
	return s.queryFiles(ctx, `
		SELECT f.id, f.location_id, f.path, f.size, f.mod_time, f.inode
		FROM files f
		LEFT JOIN file_analyses a
			ON a.file_id = f.id AND a.analyzer = ? AND a.version = ?
			AND a.size = f.size AND a.mod_time = f.mod_time
		WHERE a.file_id IS NULL AND f.mod_time <= ?
		ORDER BY f.id`,
		analyzer, version, nanos(stableBefore),
	)
}

func (s *Store) SaveAnalysis(ctx context.Context, a store.Analysis) error {
	var result any // NULL, not an empty blob, when the analysis failed
	if a.Result != nil {
		result = a.Result
	}
	_, err := s.conn(ctx).ExecContext(ctx, `
		INSERT INTO file_analyses (file_id, analyzer, version, size, mod_time, result, error, analyzed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (file_id, analyzer) DO UPDATE SET
			version = excluded.version,
			size = excluded.size,
			mod_time = excluded.mod_time,
			result = excluded.result,
			error = excluded.error,
			analyzed_at = excluded.analyzed_at`,
		a.FileID, a.Analyzer, a.Version, a.Size, nanos(a.ModTime), result, a.Error, nanos(a.AnalyzedAt),
	)
	if err != nil {
		return fmt.Errorf("store: save analysis of %s: %w", a.FileID, err)
	}
	return nil
}

func (s *Store) ListAnalyses(ctx context.Context, location uuid.UUID) ([]store.Analysis, error) {
	rows, err := s.conn(ctx).QueryContext(ctx, `
		SELECT a.file_id, a.analyzer, a.version, a.size, a.mod_time, a.result, a.error, a.analyzed_at
		FROM file_analyses a
		JOIN files f ON f.id = a.file_id AND f.size = a.size AND f.mod_time = a.mod_time
		WHERE f.location_id = ? AND a.result IS NOT NULL
		ORDER BY a.file_id, a.analyzer`,
		location,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list analyses: %w", err)
	}
	defer rows.Close()

	var analyses []store.Analysis
	for rows.Next() {
		var (
			a                   store.Analysis
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
