package identity

import (
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"

	identityv1 "github.com/lukerops/mserver/gen/identity/v1"
	"github.com/lukerops/mserver/gen/identity/v1/identityv1connect"
	"github.com/lukerops/mserver/internal/database"
	"github.com/lukerops/mserver/internal/pubsub/memory"
)

// TestModulePersists checks that the module registers its service and keeps
// its data in the database across restarts.
func TestModulePersists(t *testing.T) {
	cfg := database.Config{Driver: database.SQLite, DataDir: t.TempDir()}
	bus := memory.New()
	t.Cleanup(func() { bus.Close() })

	start := func() (*Module, identityv1connect.IdentityServiceClient) {
		t.Helper()
		server := connect.NewServer()
		m, err := New(t.Context(), cfg, server, bus, nil)
		if err != nil {
			t.Fatal(err)
		}
		transport := connectinprocess.New(server)
		return m, identityv1connect.NewIdentityServiceClient(connect.NewClient(transport))
	}

	m, c := start()
	setup, err := c.Setup(t.Context(), &identityv1.SetupRequest{
		Username: "admin",
		Email:    "admin@example.com",
		Password: "correct horse",
		Profile:  &identityv1.NewProfile{Handle: "lucas"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}

	m, c = start()
	defer m.Close()
	got, err := c.GetAccount(t.Context(), &identityv1.GetAccountRequest{Id: setup.GetAccount().GetId()})
	if err != nil || got.GetAccount().GetUsername() != "admin" {
		t.Fatalf("GetAccount after restart = %+v, %v", got, err)
	}
}
