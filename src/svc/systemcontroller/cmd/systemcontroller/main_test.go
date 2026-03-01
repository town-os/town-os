package main

import (
	"testing"
)

func TestGenerateSigningKeyReturns32Bytes(t *testing.T) {
	key, err := generateSigningKey()
	if err != nil {
		t.Fatalf("generateSigningKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d bytes", len(key))
	}
}

func TestGenerateSigningKeyUniquePerCall(t *testing.T) {
	key1, err := generateSigningKey()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	key2, err := generateSigningKey()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if string(key1) == string(key2) {
		t.Fatal("expected different keys on each call")
	}
}

func TestGenerateSigningKeyEnvOverride(t *testing.T) {
	t.Setenv("TOWN_OS_SIGNING_KEY", "env-override-key-for-testing!!")

	key, err := generateSigningKey()
	if err != nil {
		t.Fatalf("generateSigningKey: %v", err)
	}

	if string(key) != "env-override-key-for-testing!!" {
		t.Fatalf("expected env key, got %s", string(key))
	}
}
