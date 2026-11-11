package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/lukerops/mserver/internal/events"
	"github.com/lukerops/mserver/internal/modules/identity/store"
	"github.com/lukerops/mserver/internal/pubsub"
)

// publish wraps data in an envelope and publishes it. The change it
// describes is already committed, so the publication is not canceled with
// the request.
func publish[T any](ctx context.Context, pub pubsub.Publisher, typ, subject string, data T) error {
	msg, err := pubsub.Encode(pubsub.NewEnvelope(typ, events.SourceIdentity, subject, data))
	if err != nil {
		return err
	}
	return pub.Publish(context.WithoutCancel(ctx), msg)
}

func (s *Service) publishAccount(ctx context.Context, typ string, a *store.Account) error {
	return publish(ctx, s.pub, typ, a.ID.String(), events.AccountEventV1{
		AccountID: a.ID,
		Username:  a.Username,
		Email:     a.Email,
		IsAdmin:   a.IsAdmin,
		Disabled:  a.Disabled,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	})
}

func (s *Service) publishProfile(ctx context.Context, typ string, p *store.Profile) error {
	return publish(ctx, s.pub, typ, p.ID.String(), events.ProfileEventV1{
		ProfileID:   p.ID,
		AccountID:   p.AccountID,
		Handle:      p.Handle,
		DisplayName: p.DisplayName,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	})
}

func (s *Service) publishSessionRevoked(ctx context.Context, sess *store.Session, at time.Time) error {
	return publish(ctx, s.pub, events.SessionRevokedV1, sess.ID.String(), events.SessionRevokedEventV1{
		SessionID: sess.ID,
		AccountID: sess.AccountID,
		ProfileID: sess.ProfileID,
		DeviceID:  sess.DeviceID,
		RevokedAt: at,
	})
}

// logPublishError logs a failed publication after a commit. The state is
// already saved: a resync republishes it.
func (s *Service) logPublishError(ctx context.Context, typ string, err error) {
	s.log.ErrorContext(ctx, "identity: publish event failed, a resync reconciles the consumers",
		slog.String("type", typ),
		slog.String("error", err.Error()),
	)
}
