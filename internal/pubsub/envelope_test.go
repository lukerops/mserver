package pubsub

import (
	"testing"

	"github.com/google/uuid"
)

type payload struct {
	Name string `json:"name"`
}

func TestNewEnvelope(t *testing.T) {
	env := NewEnvelope("storage.file.indexed.v1", "storage", "file-1", payload{Name: "a"})

	id, err := uuid.Parse(env.ID)
	if err != nil || id.Version() != 7 {
		t.Fatalf("ID = %q, want a UUIDv7 (%v)", env.ID, err)
	}
	if env.SpecVersion != "1.0" || env.Source != "storage" || env.Subject != "file-1" || env.Time.IsZero() {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestEncodeDecode(t *testing.T) {
	env := NewEnvelope("storage.file.indexed.v1", "storage", "file-1", payload{Name: "a"})

	msg, err := Encode(env)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != env.ID || msg.Topic != env.Type {
		t.Fatalf("message = %+v, want ID and Topic of the envelope", msg)
	}

	got, err := Decode[payload](msg)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != env.ID || got.Data != env.Data || !got.Time.Equal(env.Time) {
		t.Fatalf("decoded = %+v, want %+v", got, env)
	}

	if _, err := Decode[payload](Message{Topic: "x", Payload: []byte("{")}); err == nil {
		t.Fatal("Decode of invalid JSON: want error")
	}
}
