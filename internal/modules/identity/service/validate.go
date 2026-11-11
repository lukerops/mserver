package service

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	handlePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
)

const (
	minPasswordLen    = 8
	maxPasswordLen    = 72 // bytes: bcrypt ignores the rest
	maxDisplayNameLen = 64
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidArgument}, args...)...)
}

// validateUsername accepts letters, digits, ".", "_" and "-". The "@"
// is not accepted, so a login is either a username or an email.
func validateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return invalid("username must have 1 to 64 letters, digits, '.', '_' or '-'")
	}
	return nil
}

func validateEmail(email string) error {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || addr.Name != "" {
		return invalid("invalid email %q", email)
	}
	return nil
}

func validatePassword(password string) error {
	if n := utf8.RuneCountInString(password); n < minPasswordLen || len(password) > maxPasswordLen {
		return invalid("password must have at least %d characters and at most %d bytes", minPasswordLen, maxPasswordLen)
	}
	return nil
}

// normalizeProfile lowercases the handle, trims the display name and
// defaults it to the handle.
func normalizeProfile(p NewProfile) (NewProfile, error) {
	p.Handle = strings.ToLower(strings.TrimSpace(p.Handle))
	if !handlePattern.MatchString(p.Handle) {
		return NewProfile{}, invalid("profile handle must have 1 to 32 lowercase letters, digits, '_' or '-', starting with a letter or digit")
	}
	p.DisplayName = strings.TrimSpace(p.DisplayName)
	if p.DisplayName == "" {
		p.DisplayName = p.Handle
	}
	if utf8.RuneCountInString(p.DisplayName) > maxDisplayNameLen {
		return NewProfile{}, invalid("profile display name must have at most %d characters", maxDisplayNameLen)
	}
	return p, nil
}
