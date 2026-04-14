// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package account

import (
	"errors"
	"testing"
)

func TestValidatePasswordAccepts7BitASCII(t *testing.T) {
	cases := []struct {
		name     string
		password string
	}{
		{"plain alphanumeric", "password123"},
		{"punctuation heavy", `abc!@#$%^&*()_+-=`},
		{"backticks and tildes", "`backtick`tilde~"},
		{"brackets and pipes", "{|}][abc1"},
		{"quotes and slashes", `"'/\?:;<>,.`},
		{"full visible band", `!"#$%&'()*+,-./0123456789`},
		{"letters mixed case", "MixedCase12"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validatePassword(tc.password); err != nil {
				t.Errorf("validatePassword(%q) = %v, want nil", tc.password, err)
			}
		})
	}
}

func TestValidatePasswordRejects(t *testing.T) {
	cases := []struct {
		name     string
		password string
		wantErr  error
	}{
		{"too short", "short", ErrPasswordTooShort},
		{"empty", "", ErrPasswordTooShort},
		{"space in middle", "pa ssword1", ErrPasswordInvalidChars},
		{"leading space", " password1", ErrPasswordInvalidChars},
		{"trailing space", "password1 ", ErrPasswordInvalidChars},
		{"tab", "pa\tssword1", ErrPasswordInvalidChars},
		{"newline", "pa\nssword1", ErrPasswordInvalidChars},
		{"null byte", "pa\x00ssword", ErrPasswordInvalidChars},
		{"escape", "pa\x1bssword", ErrPasswordInvalidChars},
		{"DEL byte", "password\x7f", ErrPasswordInvalidChars},
		{"latin1 umlaut", "pässword", ErrPasswordInvalidChars},
		{"latin1 high byte raw", "password\xe9", ErrPasswordInvalidChars},
		{"emoji (UTF-8 multi-byte)", "password\U0001F600", ErrPasswordInvalidChars},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePassword(tc.password)
			if err == nil {
				t.Fatalf("validatePassword(%q) = nil, want %v", tc.password, tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("validatePassword(%q) = %v, want %v", tc.password, err, tc.wantErr)
			}
		})
	}
}
