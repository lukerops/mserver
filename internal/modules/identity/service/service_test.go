package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	database "github.com/lukerops/mserver/internal/infra/database/sqlite"
	"github.com/lukerops/mserver/internal/infra/outbox"
	"github.com/lukerops/mserver/internal/infra/queue"
	"github.com/lukerops/mserver/internal/infra/queue/memory"
	"github.com/lukerops/mserver/internal/modules/identity/events"
	"github.com/lukerops/mserver/internal/modules/identity/store/sqlite"
)

func init() {
	// A cheap cost keeps the tests fast.
	hashParams = argonParams{memory: 64, time: 1, threads: 1}
}

// recorder records the types of the published events.
type recorder struct {
	mu    sync.Mutex
	types []string
	data  []json.RawMessage
}

func (r *recorder) Publish(_ context.Context, msg queue.Message) error {
	var env queue.Envelope[json.RawMessage]
	if err := json.Unmarshal(msg.Payload, &env); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types = append(r.types, env.Type)
	r.data = append(r.data, env.Data)
	return nil
}

// wait returns the recorded event types once there are at least n.
func (r *recorder) wait(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		types := slices.Clone(r.types)
		r.mu.Unlock()
		if len(types) >= n {
			return types
		}
		if time.Now().After(deadline) {
			t.Fatalf("got events %v, want %d", types, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newTestService(t *testing.T, pub queue.Publisher) (Service, *outbox.Outbox) {
	t.Helper()
	db, err := database.Open(t.Context(), filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ob, err := outbox.New(t.Context(), db, pub, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ob.Stop)
	st, err := sqlite.New(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	return New(st, ob, slog.Default()), ob
}

func newService(t *testing.T) Service {
	t.Helper()
	s, _ := newTestService(t, memory.New())
	return s
}

const password = "correct horse"

func setup(t *testing.T, s Service) SetupResult {
	t.Helper()
	res, err := s.Setup(t.Context(), SetupRequest{
		Username: "admin",
		Email:    "admin@example.com",
		Password: password,
		Profile:  NewProfile{Handle: "Lucas"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func device(id string) DeviceInfo {
	return DeviceInfo{Identifier: id, Name: "Firefox", Client: "Jellyfin Web", Version: "10.10.0"}
}

func authenticate(t *testing.T, s Service, login, dev string) AuthenticateResult {
	t.Helper()
	res, err := s.Authenticate(t.Context(), AuthenticateRequest{Login: login, Password: password, Device: device(dev)})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSetup(t *testing.T) {
	s := newService(t)
	res := setup(t, s)

	if !res.Account.IsAdmin || res.Account.Username != "admin" {
		t.Fatalf("account = %+v", res.Account)
	}
	if res.Profile.Handle != "lucas" || res.Profile.DisplayName != "lucas" || res.Profile.AccountID != res.Account.ID {
		t.Fatalf("profile = %+v", res.Profile)
	}

	_, err := s.Setup(t.Context(), SetupRequest{
		Username: "other", Email: "other@example.com", Password: password,
		Profile: NewProfile{Handle: "other"},
	})
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second setup: err = %v, want ErrAlreadyExists", err)
	}
}

func TestSetupValidation(t *testing.T) {
	valid := SetupRequest{
		Username: "admin", Email: "admin@example.com", Password: password,
		Profile: NewProfile{Handle: "lucas"},
	}
	for name, mutate := range map[string]func(*SetupRequest){
		"username": func(r *SetupRequest) { r.Username = "bad name" },
		"email":    func(r *SetupRequest) { r.Email = "not-an-email" },
		"password": func(r *SetupRequest) { r.Password = "short" },
		"handle":   func(r *SetupRequest) { r.Profile.Handle = "#bad" },
		"empty":    func(r *SetupRequest) { r.Profile.Handle = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s := newService(t)
			req := valid
			mutate(&req)
			if _, err := s.Setup(t.Context(), req); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("err = %v, want ErrInvalidArgument", err)
			}
		})
	}
}

func TestLoginInTwoSteps(t *testing.T) {
	s := newService(t)
	setup := setup(t, s)

	for _, login := range []string{"admin", "ADMIN", "Admin@Example.com"} {
		t.Run(login, func(t *testing.T) {
			res := authenticate(t, s, login, "device-"+login)
			if res.Token == "" || res.Session.AccountID != setup.Account.ID || res.Session.ProfileID != uuid.Nil {
				t.Fatalf("authenticate = %+v", res)
			}

			got, err := s.ValidateToken(t.Context(), res.Token)
			if err != nil || got.ProfileID != uuid.Nil {
				t.Fatalf("validate before select = %+v, %v", got, err)
			}

			sel, err := s.SelectProfile(t.Context(), res.Session.ID, setup.Profile.ID)
			if err != nil || sel.ProfileID != setup.Profile.ID {
				t.Fatalf("select = %+v, %v", sel, err)
			}
			got, err = s.ValidateToken(t.Context(), res.Token)
			if err != nil || got.ID != res.Session.ID || got.ProfileID != setup.Profile.ID {
				t.Fatalf("validate after select = %+v, %v", got, err)
			}
		})
	}
}

func TestAuthenticateFailures(t *testing.T) {
	s := newService(t)
	setup(t, s)

	for name, req := range map[string]AuthenticateRequest{
		"wrong password": {Login: "admin", Password: "wrong password", Device: device("d")},
		"unknown login":  {Login: "nobody", Password: password, Device: device("d")},
		"empty":          {Device: device("d")},
	} {
		if _, err := s.Authenticate(t.Context(), req); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("%s: err = %v, want ErrUnauthenticated", name, err)
		}
	}
	_, err := s.Authenticate(t.Context(), AuthenticateRequest{Login: "admin", Password: password})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("no device: err = %v, want ErrInvalidArgument", err)
	}
}

func TestSelectProfileOfAnotherAccount(t *testing.T) {
	s := newService(t)
	setup(t, s)
	res := authenticate(t, s, "admin", "d")

	if _, err := s.SelectProfile(t.Context(), res.Session.ID, newID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestNewLoginRevokesDeviceSession(t *testing.T) {
	s := newService(t)
	setup(t, s)

	first := authenticate(t, s, "admin", "d1")
	other := authenticate(t, s, "admin", "d2")
	second := authenticate(t, s, "admin", "d1")

	if _, err := s.ValidateToken(t.Context(), first.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("first token: err = %v, want ErrUnauthenticated", err)
	}
	if second.Session.DeviceID != first.Session.DeviceID {
		t.Fatalf("device changed: %v != %v", second.Session.DeviceID, first.Session.DeviceID)
	}
	for _, res := range []AuthenticateResult{other, second} {
		if _, err := s.ValidateToken(t.Context(), res.Token); err != nil {
			t.Fatalf("token of %v: %v", res.Session.DeviceID, err)
		}
	}
}

func TestRevokeSession(t *testing.T) {
	s := newService(t)
	setup(t, s)
	res := authenticate(t, s, "admin", "d")

	if err := s.RevokeSession(t.Context(), res.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidateToken(t.Context(), res.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
	if err := s.RevokeSession(t.Context(), res.Session.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke again: err = %v, want ErrNotFound", err)
	}
	if _, err := s.ValidateToken(t.Context(), ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("empty token: err = %v, want ErrUnauthenticated", err)
	}
}

func TestGetAndList(t *testing.T) {
	s := newService(t)
	res := setup(t, s)

	account, err := s.GetAccount(t.Context(), res.Account.ID)
	if err != nil || account.ID != res.Account.ID {
		t.Fatalf("account = %+v, %v", account, err)
	}
	profile, err := s.GetProfile(t.Context(), res.Profile.ID)
	if err != nil || profile.Handle != "lucas" {
		t.Fatalf("profile = %+v, %v", profile, err)
	}
	profiles, err := s.ListProfiles(t.Context(), res.Account.ID)
	if err != nil || len(profiles) != 1 || profiles[0].ID != res.Profile.ID {
		t.Fatalf("profiles = %+v, %v", profiles, err)
	}
	if _, err := s.GetAccount(t.Context(), newID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown account: err = %v", err)
	}
}

func TestEvents(t *testing.T) {
	rec := &recorder{}
	s, ob := newTestService(t, rec)

	setup(t, s)
	res := authenticate(t, s, "admin", "d")
	authenticate(t, s, "admin", "d") // revokes res
	if err := s.ResyncAccounts(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.ResyncProfiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	ob.Start(t.Context())

	want := []string{
		events.AccountCreatedV1,
		events.ProfileCreatedV1,
		events.SessionRevokedV1,
		events.AccountCreatedV1,
		events.ProfileCreatedV1,
	}
	if got := rec.wait(t, len(want)); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	var account map[string]any
	if err := json.Unmarshal(rec.data[0], &account); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"password", "password_hash"} {
		if _, ok := account[field]; ok {
			t.Errorf("account event has %q", field)
		}
	}
	var revoked events.SessionRevokedEventV1
	if err := json.Unmarshal(rec.data[2], &revoked); err != nil {
		t.Fatal(err)
	}
	if revoked.SessionID != res.Session.ID || revoked.ProfileID != uuid.Nil {
		t.Fatalf("revoked = %+v", revoked)
	}
}

func TestPasswordHash(t *testing.T) {
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := verifyPassword(hash, password); !ok || err != nil {
		t.Fatalf("verify right = %v, %v", ok, err)
	}
	if ok, err := verifyPassword(hash, "wrong"); ok || err != nil {
		t.Fatalf("verify wrong = %v, %v", ok, err)
	}
	if _, err := verifyPassword("$bcrypt$x", password); err == nil {
		t.Fatal("verify unsupported hash: no error")
	}
}
