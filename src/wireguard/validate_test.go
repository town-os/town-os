package wireguard

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// ValidateKey and ValidateEndpoint are what stop POST /networks/peers/add from
// writing a value into a document `wg-quick up` executes as root. The injection
// cases live in render_injection_test.go; these pin the shapes each validator
// accepts and refuses.

func TestValidateKeyAcceptsRealKeys(t *testing.T) {
	t.Parallel()

	for range 8 {
		_, pub, err := GenerateKeypair()
		if err != nil {
			t.Fatalf("GenerateKeypair: %v", err)
		}
		if err := ValidateKey(pub); err != nil {
			t.Fatalf("ValidateKey(%q) = %v, want accepted; this is what GenerateKeypair emits", pub, err)
		}
	}
}

func TestValidateKeyRejects(t *testing.T) {
	t.Parallel()

	thirtyOne := base64.StdEncoding.EncodeToString(make([]byte, 31))
	thirtyThree := base64.StdEncoding.EncodeToString(make([]byte, 33))

	// A key whose STANDARD encoding actually contains '+' and '/', re-spelled
	// in the URL-safe alphabet. Zero bytes will not do: they encode to all 'A's,
	// so swapping the two characters is a no-op and the "url-safe" case is
	// silently the same as a valid key.
	urlSafe := base64.URLEncoding.EncodeToString([]byte{
		0x0b, 0x30, 0x55, 0x7a, 0x9f, 0xc4, 0xe9, 0x0e,
		0x33, 0x58, 0x7d, 0xa2, 0xc7, 0xec, 0x11, 0x36,
		0x5b, 0x80, 0xa5, 0xca, 0xef, 0x14, 0x39, 0x5e,
		0x83, 0xa8, 0xcd, 0xf2, 0x17, 0x3c, 0x61, 0x86,
	})
	if !strings.ContainsAny(urlSafe, "-_") {
		t.Fatalf("fixture is wrong: %q carries no URL-safe-only character, so it is just a valid key", urlSafe)
	}

	cases := map[string]struct {
		key     string
		wantErr error
	}{
		// The shape that reaches root: a well-formed key, a newline, and a
		// second [Interface] section carrying a hook.
		"newline injection": {
			key:     "kA1yqGFbJhZ0dLpVKcQ0xJZ8k0V0m8xk5Y3xJ0Zq0Vg=\n[Interface]\nPostUp = /bin/false",
			wantErr: ErrUnsafeConfigValue,
		},
		"carriage return": {
			key:     "kA1yqGFbJhZ0dLpVKcQ0xJZ8k0V0m8xk5Y3xJ0Zq0Vg=\r\nPostUp = /bin/false",
			wantErr: ErrUnsafeConfigValue,
		},
		"null byte": {
			key:     "kA1yqGFbJhZ0dLpVKcQ0xJZ8k0V0m8xk5Y3xJ0Zq0Vg=\x00",
			wantErr: ErrUnsafeConfigValue,
		},
		// A label is not a key. Accepting one enrolls a device that can never
		// handshake and burns an overlay address.
		"placeholder label":  {key: "LABPUBKEY", wantErr: ErrInvalidKey},
		"empty":              {key: "", wantErr: ErrInvalidKey},
		"not base64":         {key: "!!!!not base64!!!!", wantErr: ErrInvalidKey},
		"base64 of 31 bytes": {key: thirtyOne, wantErr: ErrInvalidKey},
		"base64 of 33 bytes": {key: thirtyThree, wantErr: ErrInvalidKey},
		// url-safe base64 is a different alphabet; WireGuard emits standard.
		"url-safe alphabet": {key: urlSafe, wantErr: ErrInvalidKey},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := ValidateKey(tc.key)
			if err == nil {
				t.Fatalf("ValidateKey(%q) was accepted", tc.key)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("ValidateKey(%q) = %v, want %v", tc.key, err, tc.wantErr)
			}
		})
	}
}

func TestValidateEndpointAccepts(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{
		"", // no endpoint: the normal case for a phone
		"198.51.100.7:51820",
		"[2001:db8::1]:51820",
		"vpn.example.com:51820",
		"my-box.dyndns.example:1",
		"host:65535",
	} {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			if err := ValidateEndpoint(endpoint); err != nil {
				t.Errorf("ValidateEndpoint(%q) = %v, want accepted", endpoint, err)
			}
		})
	}
}

// An endpoint's host is a name something has to resolve and dial, so the same
// hostname rule applies: no underscore.
func TestValidateEndpointRejectsUnderscoreHost(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{
		"my_box.example.com:51820",
		"vpn.my_zone.example.com:51820",
		"_wireguard.example.com:51820",
	} {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			err := ValidateEndpoint(endpoint)
			if err == nil {
				t.Fatalf("ValidateEndpoint(%q) was accepted; underscore is not a hostname character", endpoint)
			}
			if !errors.Is(err, ErrInvalidEndpoint) {
				t.Errorf("ValidateEndpoint(%q) = %v, want ErrInvalidEndpoint", endpoint, err)
			}
		})
	}
}

func TestValidateEndpointRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		endpoint string
		wantErr  error
	}{
		"newline injection": {
			endpoint: "198.51.100.7:51820\n[Interface]\nPostUp = /bin/false",
			wantErr:  ErrUnsafeConfigValue,
		},
		"no port":         {endpoint: "198.51.100.7", wantErr: ErrInvalidEndpoint},
		"empty host":      {endpoint: ":51820", wantErr: ErrInvalidEndpoint},
		"empty port":      {endpoint: "198.51.100.7:", wantErr: ErrInvalidEndpoint},
		"port zero":       {endpoint: "198.51.100.7:0", wantErr: ErrInvalidEndpoint},
		"port too high":   {endpoint: "198.51.100.7:65536", wantErr: ErrInvalidEndpoint},
		"port not a port": {endpoint: "198.51.100.7:https", wantErr: ErrInvalidEndpoint},
		"unbracketed v6":  {endpoint: "2001:db8::1:51820", wantErr: ErrInvalidEndpoint},
		"leading dash":    {endpoint: "-box.example.com:51820", wantErr: ErrInvalidEndpoint},
		"trailing dash":   {endpoint: "box-.example.com:51820", wantErr: ErrInvalidEndpoint},
		"empty label":     {endpoint: "box..example.com:51820", wantErr: ErrInvalidEndpoint},
		"space in host":   {endpoint: "box example.com:51820", wantErr: ErrInvalidEndpoint},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := ValidateEndpoint(tc.endpoint)
			if err == nil {
				t.Fatalf("ValidateEndpoint(%q) was accepted", tc.endpoint)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("ValidateEndpoint(%q) = %v, want %v", tc.endpoint, err, tc.wantErr)
			}
		})
	}
}
