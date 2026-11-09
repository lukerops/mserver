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
		s.log.DebugContext(ctx, "identity: authentication rejected", "reason", "missing credentials")
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
		s.log.DebugContext(ctx, "identity: authentication rejected", "reason", "unknown login")
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
		reason := "wrong password"
		if ok {
			reason = "account disabled"
		}
		s.log.DebugContext(ctx, "identity: authentication rejected", "reason", reason, "account_id", account.ID)
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

	var revoked []Session
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
		revoked = previous
		return s.outbox.Publish(ctx, events...)
	})
	if err != nil {
		return AuthenticateResult{}, err
	}
	for _, p := range revoked {
		s.log.InfoContext(ctx, "identity: session revoked", "session_id", p.ID,
			"account_id", p.AccountID, "device_id", p.DeviceID, "reason", "new login on the device")
	}
	s.log.InfoContext(ctx, "identity: session authenticated", "session_id", session.ID,
		"account_id", session.AccountID, "device_id", session.DeviceID, "client", dev.Client)
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
			s.log.DebugContext(ctx, "identity: profile rejected, not of the session account",
				"session_id", id, "account_id", session.AccountID, "profile_id", profileID)
			return fmt.Errorf("%w: profile %s", ErrNotFound, profileID)
		}
		session.ProfileID = profile.ID
		return s.store.SetSessionProfile(ctx, id, profile.ID)
	})
	if err != nil {
		return Session{}, err
	}
	s.log.DebugContext(ctx, "identity: profile selected", "session_id", session.ID,
		"account_id", session.AccountID, "profile_id", session.ProfileID)
	return session, nil
}

func (s *service) ValidateToken(ctx context.Context, token string) (Session, error) {
	if token == "" {
		s.log.DebugContext(ctx, "identity: token rejected", "reason", "missing")
		return Session{}, fmt.Errorf("%w: missing token", ErrUnauthenticated)
	}
	session, err := s.store.FindSessionByToken(ctx, hashToken(token))
	if errors.Is(err, ErrNotFound) {
		s.log.DebugContext(ctx, "identity: token rejected", "reason", "unknown or revoked")
		return Session{}, fmt.Errorf("%w: invalid token", ErrUnauthenticated)
	}
	if err != nil {
		return Session{}, err
	}
	s.log.DebugContext(ctx, "identity: token validated", "session_id", session.ID,
		"account_id", session.AccountID, "profile_id", session.ProfileID)
	return session, nil
}

func (s *service) RevokeSession(ctx context.Context, id uuid.UUID) error {
	var session Session
	err := s.store.WithTx(ctx, func(ctx context.Context) error {
		var err error
		if session, err = s.store.GetSession(ctx, id); err != nil {
			return err
		}
		if err := s.store.DeleteSession(ctx, id); err != nil {
			return err
		}
		return s.outbox.Publish(ctx, sessionRevokedEvent(session, time.Now().UTC()))
	})
	if err != nil {
		return err
	}
	s.log.InfoContext(ctx, "identity: session revoked", "session_id", session.ID,
		"account_id", session.AccountID, "device_id", session.DeviceID)
	return nil
}
