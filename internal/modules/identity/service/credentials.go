package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// hashCost is the bcrypt cost of new hashes. Tests lower it.
var hashCost = 12

// hashPassword returns the bcrypt hash of password. It needs about 4 KiB of
// memory, so concurrent logins do not add up to a large heap. bcrypt only
// reads the first 72 bytes of the password, which validatePassword enforces
// as the maximum.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), hashCost)
	if err != nil {
		return "", fmt.Errorf("identity: hash password: %w", err)
	}
	return string(hash), nil
}

// verifyPassword reports whether password matches hash, in constant time.
// A password that does not match, or that is too long to ever match, is not
// an error.
func verifyPassword(hash, password string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword), errors.Is(err, bcrypt.ErrPasswordTooLong):
		return false, nil
	default:
		return false, fmt.Errorf("identity: verify password: %w", err)
	}
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
