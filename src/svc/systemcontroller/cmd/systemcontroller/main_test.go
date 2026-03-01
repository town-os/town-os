package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrGenerateSigningKeyCreatesFile(t *testing.T) {
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "test.db")

	key, err := loadOrGenerateSigningKey(dbFile)
	if err != nil {
		t.Fatalf("loadOrGenerateSigningKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d bytes", len(key))
	}

	// Key file should exist.
	keyPath := filepath.Join(dir, "signing-key")
	data, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key file: %v", err)
	}

	decoded, err := hex.DecodeString(string(data))
	if err != nil {
		t.Fatalf("decode key file: %v", err)
	}
	if string(decoded) != string(key) {
		t.Fatal("key file contents do not match returned key")
	}
}

func TestLoadOrGenerateSigningKeyReusesExisting(t *testing.T) {
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "test.db")

	key1, err := loadOrGenerateSigningKey(dbFile)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	key2, err := loadOrGenerateSigningKey(dbFile)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if string(key1) != string(key2) {
		t.Fatal("expected same key on second call")
	}
}

func TestLoadOrGenerateSigningKeyEnvOverride(t *testing.T) {
	dir := t.TempDir()
	dbFile := filepath.Join(dir, "test.db")

	t.Setenv("TOWN_OS_SIGNING_KEY", "env-override-key-for-testing!!")

	key, err := loadOrGenerateSigningKey(dbFile)
	if err != nil {
		t.Fatalf("loadOrGenerateSigningKey: %v", err)
	}

	if string(key) != "env-override-key-for-testing!!" {
		t.Fatalf("expected env key, got %s", string(key))
	}

	// No key file should be created.
	keyPath := filepath.Join(dir, "signing-key")
	if _, err := os.Stat(keyPath); err == nil {
		t.Fatal("expected no key file when env var is set")
	}
}

func TestLoadOrGenerateSigningKeyDifferentInstances(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	key1, err := loadOrGenerateSigningKey(filepath.Join(dir1, "test.db"))
	if err != nil {
		t.Fatalf("first instance: %v", err)
	}

	key2, err := loadOrGenerateSigningKey(filepath.Join(dir2, "test.db"))
	if err != nil {
		t.Fatalf("second instance: %v", err)
	}

	if string(key1) == string(key2) {
		t.Fatal("expected different keys for different instances")
	}
}
