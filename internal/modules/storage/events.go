package storage

import (
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/storage/api"
)

func newFileEvent(typ string, loc api.LocationID, id api.FileID, path string) queue.Envelope[api.FileEvent] {
	return queue.NewEnvelope(typ, api.Source, id.String(), api.FileEvent{
		FileID:     id,
		LocationID: loc,
		Path:       path,
	})
}
