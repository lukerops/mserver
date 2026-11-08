package client_test

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/clients"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/identity"
	"github.com/lukerops/mserver/internal/modules/identity/client"
	"github.com/lukerops/mserver/internal/modules/identity/router"
)

// TestRoundTrip runs the login in two steps through the HTTP API. The
// business rules are tested in the service package.
func TestRoundTrip(t *testing.T) {
	ctx := t.Context()
	m, err := identity.New(ctx, t.TempDir(), memory.New(), clients.Clients{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	srv := httptest.NewServer(m.Mux)
	defer srv.Close()
	c := client.New(srv.Client(), srv.URL)

	setupReq := router.SetupRequestV1{
		Username: "admin",
		Email:    "admin@example.com",
		Password: "correct horse",
		Profile:  router.NewProfileV1{Handle: "lucas", DisplayName: "Lucas"},
	}
	setup, err := c.Setup(ctx, setupReq)
	if err != nil {
		t.Fatal(err)
	}
	if setup.Account.Username != "admin" || !setup.Account.IsAdmin || setup.Profile.AccountID != setup.Account.ID {
		t.Fatalf("setup = %+v", setup)
	}
	if _, err := c.Setup(ctx, setupReq); !errors.Is(err, client.ErrAlreadyExists) {
		t.Fatalf("second setup: err = %v, want ErrAlreadyExists", err)
	}

	device := router.DeviceInfoV1{Identifier: "d1", Name: "Firefox", Client: "Jellyfin Web", Version: "10.10.0"}
	_, err = c.Authenticate(ctx, router.AuthenticateRequestV1{Login: "admin", Password: "wrong password", Device: device})
	if !errors.Is(err, client.ErrUnauthenticated) {
		t.Fatalf("wrong password: err = %v, want ErrUnauthenticated", err)
	}
	auth, err := c.Authenticate(ctx, router.AuthenticateRequestV1{Login: "admin", Password: "correct horse", Device: device})
	if err != nil {
		t.Fatal(err)
	}
	if auth.Token == "" || auth.Session.ProfileID != uuid.Nil {
		t.Fatalf("authenticate = %+v", auth)
	}

	if _, err := c.SelectProfile(ctx, auth.Session.ID, uuid.New()); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("unknown profile: err = %v, want ErrNotFound", err)
	}
	session, err := c.SelectProfile(ctx, auth.Session.ID, setup.Profile.ID)
	if err != nil || session.ProfileID != setup.Profile.ID {
		t.Fatalf("select = %+v, %v", session, err)
	}
	session, err = c.ValidateToken(ctx, auth.Token)
	if err != nil || session.ID != auth.Session.ID || session.ProfileID != setup.Profile.ID {
		t.Fatalf("validate = %+v, %v", session, err)
	}

	if account, err := c.GetAccount(ctx, setup.Account.ID); err != nil || account != setup.Account {
		t.Fatalf("account = %+v, %v", account, err)
	}
	if profile, err := c.GetProfile(ctx, setup.Profile.ID); err != nil || profile != setup.Profile {
		t.Fatalf("profile = %+v, %v", profile, err)
	}
	if profiles, err := c.ListProfiles(ctx, setup.Account.ID); err != nil || len(profiles) != 1 || profiles[0] != setup.Profile {
		t.Fatalf("profiles = %+v, %v", profiles, err)
	}
	if err := c.ResyncAccounts(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.ResyncProfiles(ctx); err != nil {
		t.Fatal(err)
	}

	if err := c.RevokeSession(ctx, auth.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ValidateToken(ctx, auth.Token); !errors.Is(err, client.ErrUnauthenticated) {
		t.Fatalf("revoked token: err = %v, want ErrUnauthenticated", err)
	}
	if _, err := c.GetAccount(ctx, uuid.New()); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("unknown account: err = %v, want ErrNotFound", err)
	}
}
