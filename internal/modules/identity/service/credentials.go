package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argonParams are the argon2id cost parameters.
type argonParams struct {
	memory  uint32 // KiB
	time    uint32
	threads uint8
}

// hashParams are used for new hashes. Existing hashes keep the parameters
// encoded in them. Tests lower the cost.
var hashParams = argonParams{memory: 64 * 1024, time: 3, threads: 4}

const (
	saltLen = 16
	keyLen  = 32
)

var b64 = base64.RawStdEncoding

// hashPassword returns the argon2id hash of password in the PHC string
// format: $argon2id$v=19$m=65536,t=3,p=4$<salt>$<key>.
func hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identity: password salt: %w", err)
	}
	p := hashParams
	key := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.time, p.threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// verifyPassword reports whether password matches hash, in constant time.
func verifyPassword(hash, password string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("identity: unsupported password hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("identity: unsupported argon2 version")
	}
	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return false, fmt.Errorf("identity: invalid argon2 parameters: %w", err)
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("identity: invalid argon2 salt: %w", err)
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("identity: invalid argon2 key: %w", err)
	}
	got := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// newToken returns a random access token and the hash that is stored.
func newToken() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("identity: token: %w", err)
	}
	token = hex.EncodeToString(b)
	return token, hashToken(token), nil
}

// hashToken returns the SHA-256 of a token, as stored in the database.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
