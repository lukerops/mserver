package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/infra/queue"
)

// dummyHash is verified when the login matches no account, so that the
// response time does not reveal which accounts exist.
var dummyHash = sync.OnceValue(func() string {
	hash, err := hashPassword("mserver-dummy-password")
	if err != nil {
		panic(err)
	}
	return hash
})

func unauthenticated() error {
	return fmt.Errorf("%w: invalid credentials", ErrUnauthenticated)
}

func (s *service) Authenticate(ctx context.Context, req AuthenticateRequest) (AuthenticateResult, error) {
	login := strings.TrimSpace(req.Login)
	if login == "" || req.Password == "" {
		return AuthenticateResult{}, unauthenticated()
	}
	dev := req.Device
	dev.Identifier = strings.TrimSpace(dev.Identifier)
	if dev.Identifier == "" {
		return AuthenticateResult{}, invalid("device identifier is required")
	}

	account, hash, err := s.store.FindAccountByLogin(ctx, login)
	if errors.Is(err, ErrNotFound) {
		_, _ = verifyPassword(dummyHash(), req.Password)
		return AuthenticateResult{}, unauthenticated()
	}
	if err != nil {
		return AuthenticateResult{}, err
	}
	ok, err := verifyPassword(hash, req.Password)
	if err != nil {
		return AuthenticateResult{}, err
	}
	if !ok || account.Disabled {
		return AuthenticateResult{}, unauthenticated()
	}

	token, tokenHash, err := newToken()
	if err != nil {
		return AuthenticateResult{}, err
	}
	now := time.Now().UTC()
	session := Session{
		ID:        newID(),
		AccountID: account.ID,
		CreatedAt: now,
	}

	err = s.store.WithTx(ctx, func(ctx context.Context) error {
		device, err := s.store.UpsertDevice(ctx, Device{
			ID:         newID(),
			AccountID:  account.ID,
			Identifier: dev.Identifier,
			Name:       dev.Name,
			Client:     dev.Client,
			Version:    dev.Version,
			CreatedAt:  now,
			LastSeenAt: now,
		})
		if err != nil {
			return err
		}
		session.DeviceID = device.ID

		// A device keeps a single session: logging in again revokes it.
		previous, err := s.store.ListDeviceSessions(ctx, device.ID)
		if err != nil {
			return err
		}
		events := make([]queue.Event, 0, len(previous))
		for _, p := range previous {
			if err := s.store.DeleteSession(ctx, p.ID); err != nil {
				return err
			}
			events = append(events, sessionRevokedEvent(p, now))
		}
		if err := s.store.InsertSession(ctx, session, tokenHash); err != nil {
			return err
		}
		return s.outbox.Publish(ctx, events...)
	})
	if err != nil {
		return AuthenticateResult{}, err
	}
	return AuthenticateResult{Session: session, Token: token}, nil
}

func (s *service) SelectProfile(ctx context.Context, id, profileID uuid.UUID) (Session, error) {
	var session Session
	err := s.store.WithTx(ctx, func(ctx context.Context) error {
		var err error
		if session, err = s.store.GetSession(ctx, id); err != nil {
			return err
		}
		profile, err := s.store.GetProfile(ctx, profileID)
		if err != nil {
			return err
		}
		if profile.AccountID != session.AccountID {
			return fmt.Errorf("%w: profile %s", ErrNotFound, profileID)
		}
		session.ProfileID = profile.ID
		return s.store.SetSessionProfile(ctx, id, profile.ID)
	})
	if err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *service) ValidateToken(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, fmt.Errorf("%w: missing token", ErrUnauthenticated)
	}
	session, err := s.store.FindSessionByToken(ctx, hashToken(token))
	if errors.Is(err, ErrNotFound) {
		return Session{}, fmt.Errorf("%w: invalid token", ErrUnauthenticated)
	}
	return session, err
}

func (s *service) RevokeSession(ctx context.Context, id uuid.UUID) error {
	return s.store.WithTx(ctx, func(ctx context.Context) error {
		session, err := s.store.GetSession(ctx, id)
		if err != nil {
			return err
		}
		if err := s.store.DeleteSession(ctx, id); err != nil {
			return err
		}
		return s.outbox.Publish(ctx, sessionRevokedEvent(session, time.Now().UTC()))
	})
}
