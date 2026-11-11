package store

import (
	"context"
	"database/sql"
	"fmt"
	"path"

	"github.com/google/uuid"
)

// ApplyScan applies the changes of a scan atomically.
func (s *Store) ApplyScan(ctx context.Context, location uuid.UUID, c *ScanChanges) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for _, id := range c.Removed {
			if _, err := tx.ExecContext(ctx, deleteFile, id); err != nil {
				return fmt.Errorf("store: remove file %s: %w", id, err)
			}
		}
		for id, info := range c.Updated {
			if _, err := tx.ExecContext(ctx, updateFile,
				info.Path, path.Dir(info.Path), info.Size, nanos(info.ModTime), int64(info.Inode), id,
			); err != nil {
				return fmt.Errorf("store: update %s: %w", info.Path, err)
			}
		}
		for id, info := range c.Added {
			if _, err := tx.ExecContext(ctx, insertFile,
				id, location, info.Path, path.Dir(info.Path),
				info.Size, nanos(info.ModTime), int64(info.Inode),
			); err != nil {
				return fmt.Errorf("store: add %s: %w", info.Path, err)
			}
		}
		return nil
	})
}
