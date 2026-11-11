package handler

import (
	"io"
	"log/slog"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
	"github.com/lukerops/mserver/gen/identity/v1/identityv1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/modules/identity/service"
	"github.com/lukerops/mserver/internal/modules/identity/store"
	"github.com/lukerops/mserver/internal/pubsub/memory"
)

// newTest returns a client of the handler through the in-process
// transport.
func newTest(t *testing.T) identityv1connect.IdentityServiceClient {
	t.Helper()
	cfg := database.Config{Driver: database.SQLite, DataDir: ":memory:"}
	db, err := database.Open(t.Context(), cfg, "identity", store.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	bus := memory.New()
	t.Cleanup(func() { bus.Close() })

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := New(service.New(store.New(db), bus, log), log)

	server := connect.NewServer()
	identityv1connect.RegisterIdentityServiceHandler(server, h)
	return identityv1connect.NewIdentityServiceClient(connect.NewClient(connectinprocess.New(server)))
}

func wantCode(t *testing.T, what string, err error, want connect.Code) {
	t.Helper()
	if got := connect.CodeOf(err); got != want {
		t.Errorf("%s: code = %v, want %v (%v)", what, got, want, err)
	}
}

// TestRoundTrip runs the login in two steps through the ConnectRPC API. The
// business rules are tested in the service package.
func TestRoundTrip(t *testing.T) {
	ctx := t.Context()
	c := newTest(t)

	setupReq := &identityv1.SetupRequest{
		Username: "admin",
		Email:    "admin@example.com",
		Password: "correct horse",
		Profile:  &identityv1.NewProfile{Handle: "lucas", DisplayName: "Lucas"},
	}
	setup, err := c.Setup(ctx, setupReq)
	if err != nil {
		t.Fatal(err)
	}
	account, profile := setup.GetAccount(), setup.GetProfile()
	if account.GetUsername() != "admin" || !account.GetIsAdmin() || profile.GetAccountId() != account.GetId() {
		t.Fatalf("setup = %+v", setup)
	}
	_, err = c.Setup(ctx, setupReq)
	wantCode(t, "second setup", err, connect.CodeAlreadyExists)

	device := &identityv1.DeviceInfo{Identifier: "d1", Name: "Firefox", Client: "Jellyfin Web", Version: "10.10.0"}
	_, err = c.Authenticate(ctx, &identityv1.AuthenticateRequest{Login: "admin", Password: "wrong password", Device: device})
	wantCode(t, "wrong password", err, connect.CodeUnauthenticated)
	auth, err := c.Authenticate(ctx, &identityv1.AuthenticateRequest{Login: "admin", Password: "correct horse", Device: device})
	if err != nil {
		t.Fatal(err)
	}
	session := auth.GetSession()
	if auth.GetToken() == "" || session.GetProfileId() != "" {
		t.Fatalf("authenticate = %+v", auth)
	}

	_, err = c.SelectProfile(ctx, &identityv1.SelectProfileRequest{SessionId: session.GetId(), ProfileId: uuid.NewString()})
	wantCode(t, "unknown profile", err, connect.CodeNotFound)
	_, err = c.SelectProfile(ctx, &identityv1.SelectProfileRequest{SessionId: session.GetId(), ProfileId: "not-a-uuid"})
	wantCode(t, "invalid profile id", err, connect.CodeInvalidArgument)
	selected, err := c.SelectProfile(ctx, &identityv1.SelectProfileRequest{SessionId: session.GetId(), ProfileId: profile.GetId()})
	if err != nil || selected.GetSession().GetProfileId() != profile.GetId() {
		t.Fatalf("select = %+v, %v", selected, err)
	}
	validated, err := c.ValidateToken(ctx, &identityv1.ValidateTokenRequest{Token: auth.GetToken()})
	if err != nil || validated.GetSession().GetId() != session.GetId() || validated.GetSession().GetProfileId() != profile.GetId() {
		t.Fatalf("validate = %+v, %v", validated, err)
	}

	gotAccount, err := c.GetAccount(ctx, &identityv1.GetAccountRequest{Id: account.GetId()})
	if err != nil || !proto.Equal(gotAccount.GetAccount(), account) {
		t.Fatalf("account = %+v, %v", gotAccount, err)
	}
	gotProfile, err := c.GetProfile(ctx, &identityv1.GetProfileRequest{Id: profile.GetId()})
	if err != nil || !proto.Equal(gotProfile.GetProfile(), profile) {
		t.Fatalf("profile = %+v, %v", gotProfile, err)
	}
	list, err := c.ListProfiles(ctx, &identityv1.ListProfilesRequest{AccountId: account.GetId()})
	if err != nil || len(list.GetProfiles()) != 1 || !proto.Equal(list.GetProfiles()[0], profile) {
		t.Fatalf("profiles = %+v, %v", list, err)
	}
	if _, err := c.ResyncAccounts(ctx, &identityv1.ResyncAccountsRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ResyncProfiles(ctx, &identityv1.ResyncProfilesRequest{}); err != nil {
		t.Fatal(err)
	}

	if _, err := c.RevokeSession(ctx, &identityv1.RevokeSessionRequest{SessionId: session.GetId()}); err != nil {
		t.Fatal(err)
	}
	_, err = c.ValidateToken(ctx, &identityv1.ValidateTokenRequest{Token: auth.GetToken()})
	wantCode(t, "revoked token", err, connect.CodeUnauthenticated)
	_, err = c.RevokeSession(ctx, &identityv1.RevokeSessionRequest{SessionId: session.GetId()})
	wantCode(t, "revoke again", err, connect.CodeNotFound)
	_, err = c.GetAccount(ctx, &identityv1.GetAccountRequest{Id: uuid.NewString()})
	wantCode(t, "unknown account", err, connect.CodeNotFound)
}

func TestInvalidArguments(t *testing.T) {
	ctx := t.Context()
	c := newTest(t)

	_, err := c.Setup(ctx, &identityv1.SetupRequest{Username: "bad name"})
	wantCode(t, "setup", err, connect.CodeInvalidArgument)
	_, err = c.GetAccount(ctx, &identityv1.GetAccountRequest{Id: "x"})
	wantCode(t, "get account", err, connect.CodeInvalidArgument)
	_, err = c.GetProfile(ctx, &identityv1.GetProfileRequest{})
	wantCode(t, "get profile", err, connect.CodeInvalidArgument)
	_, err = c.ListProfiles(ctx, &identityv1.ListProfilesRequest{AccountId: "x"})
	wantCode(t, "list profiles", err, connect.CodeInvalidArgument)
	_, err = c.RevokeSession(ctx, &identityv1.RevokeSessionRequest{SessionId: "x"})
	wantCode(t, "revoke session", err, connect.CodeInvalidArgument)
}
