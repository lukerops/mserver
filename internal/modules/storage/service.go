package storage

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/modules/storage/api"
	"github.com/lukerops/mserver/internal/modules/storage/driver"
	"github.com/lukerops/mserver/internal/modules/storage/driver/local"
	"github.com/lukerops/mserver/internal/modules/storage/store"
)

// factories are the available drivers.
var factories = map[string]driver.Factory{
	local.Name: local.New,
}

// Service is the local implementation of api.Service.
type Service struct {
	store  store.Store
	outbox *outbox.Outbox
	log    *slog.Logger

	// scanMu serializes scans and location deletions.
	scanMu sync.Mutex
}

var _ api.Service = (*Service)(nil)

func newDriver(name string, options map[string]string) (driver.Driver, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("%w: unknown driver %q", api.ErrInvalidArgument, name)
	}
	return factory(options)
}
