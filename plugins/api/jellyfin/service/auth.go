package service

import (
	"context"
	"log/slog"
	"strings"

	"connectrpc.com/connect/v2"
	"github.com/google/uuid"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
)

// Authenticate checks the token of a request and returns its caller.
func (s *Service) Authenticate(ctx context.Context, token string) (Caller, error) {
	res, err := s.identity.ValidateToken(ctx, &identityv1.ValidateTokenRequest{Token: token})
	if err != nil {
		switch code := connect.CodeOf(err); {
		case token == "" && code == connect.CodeUnauthenticated:
			return Caller{}, errUnauthenticated("missing token")
		case code == connect.CodeUnauthenticated, code == connect.CodeNotFound:
			return Caller{}, errUnauthenticated("invalid token: " + err.Error())
		default:
			return Caller{}, err
		}
	}
	session := res.GetSession()
	if session.GetProfileId() == "" {
		return Caller{}, errUnauthenticated("session has no profile selected")
	}

	account, err := s.identity.GetAccount(ctx, &identityv1.GetAccountRequest{Id: session.GetAccountId()})
	switch {
	case isCode(err, connect.CodeUnauthenticated), isCode(err, connect.CodeNotFound):
		return Caller{}, errUnauthenticated("account not found")
	case err != nil:
		return Caller{}, err
	case account.GetAccount().GetDisabled():
		return Caller{}, errUnauthenticated("account disabled")
	}

	profile, err := s.identity.GetProfile(ctx, &identityv1.GetProfileRequest{Id: session.GetProfileId()})
	switch {
	case isCode(err, connect.CodeUnauthenticated), isCode(err, connect.CodeNotFound):
		return Caller{}, errUnauthenticated("profile not found")
	case err != nil:
		return Caller{}, err
	}

	return Caller{
		SessionID:          session.GetId(),
		AccountID:          session.GetAccountId(),
		ProfileID:          session.GetProfileId(),
		DeviceID:           session.GetDeviceId(),
		AccountDisabled:    account.GetAccount().GetDisabled(),
		ProfileDisplayName: profile.GetProfile().GetDisplayName(),
	}, nil
}

// Login authenticates the account with login and password, then selects
// the profile with handle on the same session. It returns the caller and
// the access token.
func (s *Service) Login(ctx context.Context, login, password, handle string, d Device) (Caller, string, error) {
	res, err := s.identity.Authenticate(ctx, &identityv1.AuthenticateRequest{
		Login:    login,
		Password: password,
		Device: &identityv1.DeviceInfo{
			Identifier: d.ID,
			Name:       d.Name,
			Client:     d.Client,
			Version:    d.Version,
		},
	})
	switch code := connect.CodeOf(err); {
	case err == nil:
	case code == connect.CodeUnauthenticated:
		// The identity does not tell an unknown login from a wrong password.
		return Caller{}, "", errUnauthenticated("unknown user or wrong password")
	case code == connect.CodeInvalidArgument:
		return Caller{}, "", errInvalidArgument(err.Error())
	default:
		return Caller{}, "", err
	}

	c, err := s.selectProfile(ctx, res.GetSession(), handle)
	if err != nil {
		// The account session must not outlive a failed login.
		_, revokeErr := s.identity.RevokeSession(context.WithoutCancel(ctx),
			&identityv1.RevokeSessionRequest{SessionId: res.GetSession().GetId()})
		if revokeErr != nil {
			s.log.ErrorContext(ctx, "jellyfin: revoke session",
				slog.String("session_id", res.GetSession().GetId()),
				slog.String("error", revokeErr.Error()),
			)
		}
		if isCode(err, connect.CodeNotFound) {
			return Caller{}, "", errUnauthenticated("unknown profile")
		}
		return Caller{}, "", err
	}

	s.log.InfoContext(ctx, "jellyfin: user authenticated",
		slog.String("account_id", c.AccountID),
		slog.String("profile_id", c.ProfileID),
		slog.String("session_id", c.SessionID),
		slog.String("client", d.Client),
		slog.String("device", d.Name),
	)
	return c, res.GetToken(), nil
}

// selectProfile finds the profile of the session account by handle (case
// insensitive) and binds it to the session. It fails with the NOT_FOUND
// code of the identity when the account has no such profile.
func (s *Service) selectProfile(ctx context.Context, session *identityv1.Session, handle string) (Caller, error) {
	account, err := s.identity.GetAccount(ctx, &identityv1.GetAccountRequest{Id: session.GetAccountId()})
	if err != nil {
		return Caller{}, err
	}
	profiles, err := s.identity.ListProfiles(ctx, &identityv1.ListProfilesRequest{AccountId: session.GetAccountId()})
	if err != nil {
		return Caller{}, err
	}
	for _, profile := range profiles.GetProfiles() {
		if !strings.EqualFold(profile.GetHandle(), handle) {
			continue
		}
		selected, err := s.identity.SelectProfile(ctx, &identityv1.SelectProfileRequest{
			SessionId: session.GetId(),
			ProfileId: profile.GetId(),
		})
		if err != nil {
			return Caller{}, err
		}
		return Caller{
			SessionID:          selected.GetSession().GetId(),
			AccountID:          selected.GetSession().GetAccountId(),
			ProfileID:          selected.GetSession().GetProfileId(),
			DeviceID:           selected.GetSession().GetDeviceId(),
			AccountDisabled:    account.GetAccount().GetDisabled(),
			ProfileDisplayName: profile.GetDisplayName(),
		}, nil
	}
	return Caller{}, connect.NewError(connect.CodeNotFound, "profile not found")
}

// Logout revokes the session of the caller.
func (s *Service) Logout(ctx context.Context, c Caller) error {
	if _, err := s.identity.RevokeSession(ctx, &identityv1.RevokeSessionRequest{SessionId: c.SessionID}); err != nil {
		return err
	}
	s.log.InfoContext(ctx, "jellyfin: user logged out",
		slog.String("account_id", c.AccountID),
		slog.String("profile_id", c.ProfileID),
		slog.String("session_id", c.SessionID),
	)
	return nil
}

// UserByID returns a profile of the caller account.
func (s *Service) UserByID(ctx context.Context, c Caller, id uuid.UUID) (User, error) {
	res, err := s.identity.GetProfile(ctx, &identityv1.GetProfileRequest{Id: id.String()})
	if isCode(err, connect.CodeNotFound) {
		return User{}, errNotFound("user not found")
	}
	if err != nil {
		return User{}, err
	}
	profile := res.GetProfile()
	if profile.GetAccountId() != c.AccountID {
		return User{}, errNotFound("user of another account")
	}
	return user(c.AccountDisabled, profile.GetId(), profile.GetDisplayName()), nil
}
