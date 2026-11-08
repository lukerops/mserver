package service

import (
	"time"

	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/modules/identity/events"
	"github.com/lukerops/mserver/internal/modules/identity/store"
)

func accountEvent(typ string, a store.Account) queue.Envelope[events.AccountEventV1] {
	return queue.NewEnvelope(typ, events.Source, a.ID.String(), events.AccountEventV1{
		AccountID: a.ID,
		Username:  a.Username,
		Email:     a.Email,
		IsAdmin:   a.IsAdmin,
		Disabled:  a.Disabled,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	})
}

func profileEvent(typ string, p store.Profile) queue.Envelope[events.ProfileEventV1] {
	return queue.NewEnvelope(typ, events.Source, p.ID.String(), events.ProfileEventV1{
		ProfileID:   p.ID,
		AccountID:   p.AccountID,
		Handle:      p.Handle,
		DisplayName: p.DisplayName,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	})
}

func sessionRevokedEvent(s store.Session, at time.Time) queue.Envelope[events.SessionRevokedEventV1] {
	return queue.NewEnvelope(events.SessionRevokedV1, events.Source, s.ID.String(), events.SessionRevokedEventV1{
		SessionID: s.ID,
		AccountID: s.AccountID,
		ProfileID: s.ProfileID,
		DeviceID:  s.DeviceID,
		RevokedAt: at,
	})
}
