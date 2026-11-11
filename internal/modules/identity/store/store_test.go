package store

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lukerops/mserver/internal/database"
)

// TestStore runs the same tests on every available database: SQLite in
// memory, and Postgres when MSERVER_TEST_POSTGRES_URL points to a
// disposable database.
func TestStore(t *testing.T) {
	backends := map[string]func(t *testing.T) *Store{
		"sqlite": func(t *testing.T) *Store {
			return openStore(t, database.Config{Driver: database.SQLite, DataDir: ":memory:"})
		},
	}
	if url := os.Getenv("MSERVER_TEST_POSTGRES_URL"); url != "" {
		backends["postgres"] = func(t *testing.T) *Store {
			return openStore(t, database.Config{Driver: database.Postgres, PostgresURL: url})
		}
	}

	tests := map[string]func(t *testing.T, st *Store){
		"first account":  testFirstAccount,
		"find by login":  testFindByLogin,
		"profiles":       testProfiles,
		"open session":   testOpenSession,
		"select profile": testSelectProfile,
		"delete session": testDeleteSession,
	}
	for backend, open := range backends {
		for name, test := range tests {
			t.Run(backend+"/"+name, func(t *testing.T) { test(t, open(t)) })
		}
	}
}

func openStore(t *testing.T, cfg database.Config) *Store {
	t.Helper()
	ctx := t.Context()
	db, err := database.Open(ctx, cfg, "identity", Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cfg.Driver == database.Postgres {
			db.ExecContext(ctx, `DROP SCHEMA identity CASCADE`)
		}
		db.Close()
	})
	return New(db)
}

func newID() uuid.UUID { return uuid.Must(uuid.NewV7()) }

var epoch = time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)

func newAccount(username string) (*Account, *Profile) {
	a := &Account{
		ID:        newID(),
		Username:  username,
		Email:     username + "@Example.com",
		IsAdmin:   true,
		CreatedAt: epoch,
		UpdatedAt: epoch.Add(time.Second),
	}
	p := &Profile{
		ID:          newID(),
		AccountID:   a.ID,
		Handle:      "lucas",
		DisplayName: "Lucas",
		CreatedAt:   epoch,
		UpdatedAt:   epoch.Add(time.Second),
	}
	return a, p
}

func mustCreateFirstAccount(t *testing.T, st *Store) (*Account, *Profile) {
	t.Helper()
	a, p := newAccount("admin")
	if err := st.CreateFirstAccount(t.Context(), a, "hash", p); err != nil {
		t.Fatal(err)
	}
	return a, p
}

func newDevice(account uuid.UUID, identifier string) *Device {
	return &Device{
		ID:         newID(),
		AccountID:  account,
		Identifier: identifier,
		Name:       "Firefox",
		Client:     "Jellyfin Web",
		Version:    "10.10.0",
		CreatedAt:  epoch,
		LastSeenAt: epoch,
	}
}

func openSession(t *testing.T, st *Store, account uuid.UUID, identifier, tokenHash string) (*Device, *Session, []Session) {
	t.Helper()
	d := newDevice(account, identifier)
	s := &Session{ID: newID(), AccountID: account, CreatedAt: epoch}
	revoked, err := st.OpenSession(t.Context(), d, s, tokenHash)
	if err != nil {
		t.Fatal(err)
	}
	return d, s, revoked
}

func testFirstAccount(t *testing.T, st *Store) {
	ctx := t.Context()
	a, _ := mustCreateFirstAccount(t, st)

	got, err := st.GetAccount(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *a {
		t.Errorf("GetAccount = %+v, want %+v", got, a)
	}
	if _, err := st.GetAccount(ctx, newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAccount(unknown): err = %v, want ErrNotFound", err)
	}

	other, otherProfile := newAccount("other")
	if err := st.CreateFirstAccount(ctx, other, "hash", otherProfile); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("second CreateFirstAccount: err = %v, want ErrAlreadyExists", err)
	}
	if _, err := st.GetAccount(ctx, other.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("rejected account was saved: err = %v", err)
	}

	accounts, err := st.ListAccounts(ctx)
	if err != nil || len(accounts) != 1 || accounts[0].ID != a.ID {
		t.Errorf("ListAccounts = %+v, %v", accounts, err)
	}
}

func testFindByLogin(t *testing.T, st *Store) {
	ctx := t.Context()
	a, _ := mustCreateFirstAccount(t, st)

	for _, login := range []string{"admin", "ADMIN", "admin@example.com", "Admin@EXAMPLE.com"} {
		got, hash, err := st.FindAccountByLogin(ctx, login)
		if err != nil {
			t.Fatalf("FindAccountByLogin(%q): %v", login, err)
		}
		if *got != *a || hash != "hash" {
			t.Errorf("FindAccountByLogin(%q) = %+v, %q", login, got, hash)
		}
	}
	if _, _, err := st.FindAccountByLogin(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindAccountByLogin(unknown): err = %v, want ErrNotFound", err)
	}
}

func testProfiles(t *testing.T, st *Store) {
	ctx := t.Context()
	a, p := mustCreateFirstAccount(t, st)

	got, err := st.GetProfile(ctx, p.ID)
	if err != nil || *got != *p {
		t.Errorf("GetProfile = %+v, %v; want %+v", got, err, p)
	}
	if _, err := st.GetProfile(ctx, newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetProfile(unknown): err = %v, want ErrNotFound", err)
	}
	for name, list := range map[string]func() ([]Profile, error){
		"ListProfiles":    func() ([]Profile, error) { return st.ListProfiles(ctx, a.ID) },
		"ListAllProfiles": func() ([]Profile, error) { return st.ListAllProfiles(ctx) },
	} {
		profiles, err := list()
		if err != nil || len(profiles) != 1 || profiles[0] != *p {
			t.Errorf("%s = %+v, %v", name, profiles, err)
		}
	}
	if profiles, err := st.ListProfiles(ctx, newID()); err != nil || len(profiles) != 0 {
		t.Errorf("ListProfiles(unknown account) = %+v, %v", profiles, err)
	}
}

func testOpenSession(t *testing.T, st *Store) {
	ctx := t.Context()
	a, _ := mustCreateFirstAccount(t, st)

	d1, s1, revoked := openSession(t, st, a.ID, "d1", "token-1")
	if len(revoked) != 0 || s1.DeviceID != d1.ID {
		t.Fatalf("first login: device = %v, session = %+v, revoked = %+v", d1.ID, s1, revoked)
	}
	got, err := st.FindSessionByToken(ctx, "token-1")
	if err != nil || *got != *s1 {
		t.Fatalf("FindSessionByToken = %+v, %v; want %+v", got, err, s1)
	}

	// Another device does not revoke the session of the first one.
	_, _, revoked = openSession(t, st, a.ID, "d2", "token-2")
	if len(revoked) != 0 {
		t.Fatalf("other device revoked %+v", revoked)
	}

	// The same device keeps its ID and revokes its previous session.
	d3, s3, revoked := openSession(t, st, a.ID, "d1", "token-3")
	if d3.ID != d1.ID || s3.DeviceID != d1.ID {
		t.Errorf("device = %v, session device = %v, want %v", d3.ID, s3.DeviceID, d1.ID)
	}
	if !d3.CreatedAt.Equal(epoch) {
		t.Errorf("device created at %v, want %v", d3.CreatedAt, epoch)
	}
	if len(revoked) != 1 || revoked[0] != *s1 {
		t.Fatalf("revoked = %+v, want [%+v]", revoked, s1)
	}
	if _, err := st.FindSessionByToken(ctx, "token-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked token: err = %v, want ErrNotFound", err)
	}
	if _, err := st.FindSessionByToken(ctx, "token-3"); err != nil {
		t.Errorf("new token: %v", err)
	}
}

func testSelectProfile(t *testing.T, st *Store) {
	ctx := t.Context()
	a, p := mustCreateFirstAccount(t, st)
	_, s, _ := openSession(t, st, a.ID, "d", "token")

	got, err := st.SelectSessionProfile(ctx, s.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != s.ID || got.ProfileID != p.ID {
		t.Errorf("SelectSessionProfile = %+v", got)
	}
	found, err := st.FindSessionByToken(ctx, "token")
	if err != nil || found.ProfileID != p.ID {
		t.Errorf("FindSessionByToken = %+v, %v", found, err)
	}

	// A profile of another account cannot be selected.
	other, otherProfile := newAccount("other")
	if _, err := st.db.ExecContext(ctx, insertAccount,
		other.ID, other.Username, key(other.Username), other.Email, key(other.Email), "hash",
		other.IsAdmin, other.Disabled, nanos(other.CreatedAt), nanos(other.UpdatedAt)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, insertProfile,
		otherProfile.ID, otherProfile.AccountID, otherProfile.Handle, otherProfile.DisplayName,
		nanos(otherProfile.CreatedAt), nanos(otherProfile.UpdatedAt)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SelectSessionProfile(ctx, s.ID, otherProfile.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("profile of another account: err = %v, want ErrNotFound", err)
	}
	if _, err := st.SelectSessionProfile(ctx, s.ID, newID()); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown profile: err = %v, want ErrNotFound", err)
	}
	if _, err := st.SelectSessionProfile(ctx, newID(), p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session: err = %v, want ErrNotFound", err)
	}
	// The rejected attempts left the selected profile alone.
	if found, err := st.FindSessionByToken(ctx, "token"); err != nil || found.ProfileID != p.ID {
		t.Errorf("FindSessionByToken = %+v, %v", found, err)
	}
}

func testDeleteSession(t *testing.T, st *Store) {
	ctx := t.Context()
	a, _ := mustCreateFirstAccount(t, st)
	_, s, _ := openSession(t, st, a.ID, "d", "token")

	got, err := st.DeleteSession(ctx, s.ID)
	if err != nil || *got != *s {
		t.Fatalf("DeleteSession = %+v, %v; want %+v", got, err, s)
	}
	if _, err := st.FindSessionByToken(ctx, "token"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted session: err = %v, want ErrNotFound", err)
	}
	if _, err := st.DeleteSession(ctx, s.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete again: err = %v, want ErrNotFound", err)
	}
}
