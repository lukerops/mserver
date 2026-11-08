package sqlite

import (
	"context"
	"fmt"

	"github.com/lukerops/mserver/internal/modules/identity/store"
)

func (s *Store) UpsertDevice(ctx context.Context, d store.Device) (store.Device, error) {
	var createdAt int64
	err := s.conn(ctx).QueryRowContext(ctx,
		`INSERT INTO devices (id, account_id, identifier, name, client, version, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (account_id, identifier) DO UPDATE SET
			name = excluded.name,
			client = excluded.client,
			version = excluded.version,
			last_seen_at = excluded.last_seen_at
		RETURNING id, created_at`,
		d.ID, d.AccountID, d.Identifier, d.Name, d.Client, d.Version,
		nanos(d.CreatedAt), nanos(d.LastSeenAt),
	).Scan(&d.ID, &createdAt)
	if err != nil {
		return store.Device{}, fmt.Errorf("store: upsert device: %w", err)
	}
	d.CreatedAt = fromNanos(createdAt)
	return d, nil
}
