package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/events"
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

// Authenticate checks the credentials of an account and opens a session on
// the device, registering it if needed. A previous session of the same
// device is revoked. Wrong credentials and disabled accounts return
// ErrUnauthenticated.
func (s *Service) Authenticate(ctx context.Context, req *AuthenticateRequest) (*AuthenticateResult, error) {
	login := strings.TrimSpace(req.Login)
	if login == "" || req.Password == "" {
		s.logRejected(ctx, "missing credentials")
		return nil, unauthenticated()
	}
	identifier := strings.TrimSpace(req.Device.Identifier)
	if identifier == "" {
		return nil, invalid("device identifier is required")
	}

	account, hash, err := s.store.FindAccountByLogin(ctx, login)
	if errors.Is(err, ErrNotFound) {
		_, _ = verifyPassword(dummyHash(), req.Password)
		s.logRejected(ctx, "unknown login")
		return nil, unauthenticated()
	}
	if err != nil {
		return nil, err
	}
	ok, err := verifyPassword(hash, req.Password)
	if err != nil {
		return nil, err
	}
	if !ok || account.Disabled {
		reason := "wrong password"
		if ok {
			reason = "account disabled"
		}
		s.logRejected(ctx, reason, slog.String("account_id", account.ID.String()))
		return nil, unauthenticated()
	}

	token, tokenHash, err := newToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	device := &Device{
		ID:         newID(),
		AccountID:  account.ID,
		Identifier: identifier,
		Name:       req.Device.Name,
		Client:     req.Device.Client,
		Version:    req.Device.Version,
		CreatedAt:  now,
		LastSeenAt: now,
	}
	res := &AuthenticateResult{
		Session: Session{ID: newID(), AccountID: account.ID, CreatedAt: now},
		Token:   token,
	}

	// A device keeps a single session: logging in again revokes it.
	revoked, err := s.store.OpenSession(ctx, device, &res.Session, tokenHash)
	if err != nil {
		return nil, err
	}
	for i := range revoked {
		r := &revoked[i]
		s.log.InfoContext(ctx, "identity: session revoked",
			slog.String("session_id", r.ID.String()),
			slog.String("account_id", r.AccountID.String()),
			slog.String("device_id", r.DeviceID.String()),
			slog.String("reason", "new login on the device"),
		)
		if err := s.publishSessionRevoked(ctx, r, now); err != nil {
			s.logPublishError(ctx, events.SessionRevokedV1, err)
		}
	}
	s.log.InfoContext(ctx, "identity: session authenticated",
		slog.String("session_id", res.Session.ID.String()),
		slog.String("account_id", res.Session.AccountID.String()),
		slog.String("device_id", res.Session.DeviceID.String()),
		slog.String("client", req.Device.Client),
	)
	return res, nil
}

func (s *Service) logRejected(ctx context.Context, reason string, attrs ...slog.Attr) {
	attrs = append(attrs, slog.String("reason", reason))
	s.log.LogAttrs(ctx, slog.LevelDebug, "identity: authentication rejected", attrs...)
}

// SelectProfile binds a profile of the session account to the session. It
// returns ErrNotFound if the session does not exist or the profile is not
// of that account.
func (s *Service) SelectProfile(ctx context.Context, id, profile uuid.UUID) (*Session, error) {
	session, err := s.store.SelectSessionProfile(ctx, id, profile)
	if err != nil {
		return nil, err
	}
	s.log.DebugContext(ctx, "identity: profile selected",
		slog.String("session_id", session.ID.String()),
		slog.String("account_id", session.AccountID.String()),
		slog.String("profile_id", session.ProfileID.String()),
	)
	return session, nil
}

// ValidateToken returns the session of an access token, or
// ErrUnauthenticated.
func (s *Service) ValidateToken(ctx context.Context, token string) (*Session, error) {
	if token == "" {
		s.log.DebugContext(ctx, "identity: token rejected", slog.String("reason", "missing"))
		return nil, fmt.Errorf("%w: missing token", ErrUnauthenticated)
	}
	session, err := s.store.FindSessionByToken(ctx, hashToken(token))
	if errors.Is(err, ErrNotFound) {
		s.log.DebugContext(ctx, "identity: token rejected", slog.String("reason", "unknown or revoked"))
		return nil, fmt.Errorf("%w: invalid token", ErrUnauthenticated)
	}
	if err != nil {
		return nil, err
	}
	s.log.DebugContext(ctx, "identity: token validated",
		slog.String("session_id", session.ID.String()),
		slog.String("account_id", session.AccountID.String()),
		slog.String("profile_id", session.ProfileID.String()),
	)
	return session, nil
}

// RevokeSession ends a session; its token stops being valid. It returns
// ErrNotFound if the session does not exist.
func (s *Service) RevokeSession(ctx context.Context, id uuid.UUID) error {
	session, err := s.store.DeleteSession(ctx, id)
	if err != nil {
		return err
	}
	s.log.InfoContext(ctx, "identity: session revoked",
		slog.String("session_id", session.ID.String()),
		slog.String("account_id", session.AccountID.String()),
		slog.String("device_id", session.DeviceID.String()),
	)
	if err := s.publishSessionRevoked(ctx, session, time.Now().UTC()); err != nil {
		s.logPublishError(ctx, events.SessionRevokedV1, err)
	}
	return nil
}
