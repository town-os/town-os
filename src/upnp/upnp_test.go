package upnp

import (
	"errors"
	"testing"
)

func TestMockManagerRecordsCalls(t *testing.T) {
	m := &MockManager{}

	if err := m.AddPortMapping("TCP", 8080, 80, "test", 600); err != nil {
		t.Fatalf("AddPortMapping: %v", err)
	}
	if err := m.RemovePortMapping("TCP", 8080); err != nil {
		t.Fatalf("RemovePortMapping: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	if calls[0].Method != "AddPortMapping" {
		t.Fatalf("expected AddPortMapping, got %s", calls[0].Method)
	}
	if calls[0].Args[0].(string) != "TCP" {
		t.Fatalf("expected protocol TCP, got %v", calls[0].Args[0])
	}
	if calls[0].Args[1].(uint16) != 8080 {
		t.Fatalf("expected external port 8080, got %v", calls[0].Args[1])
	}
	if calls[0].Args[2].(uint16) != 80 {
		t.Fatalf("expected internal port 80, got %v", calls[0].Args[2])
	}
	if calls[0].Args[3].(string) != "test" {
		t.Fatalf("expected description test, got %v", calls[0].Args[3])
	}
	if calls[0].Args[4].(uint32) != 600 {
		t.Fatalf("expected ttl 600, got %v", calls[0].Args[4])
	}

	if calls[1].Method != "RemovePortMapping" {
		t.Fatalf("expected RemovePortMapping, got %s", calls[1].Method)
	}
	if calls[1].Args[0].(string) != "TCP" {
		t.Fatalf("expected protocol TCP, got %v", calls[1].Args[0])
	}
	if calls[1].Args[1].(uint16) != 8080 {
		t.Fatalf("expected external port 8080, got %v", calls[1].Args[1])
	}
}

func TestMockManagerAddError(t *testing.T) {
	injected := errors.New("add failed")
	m := &MockManager{AddErr: injected}

	err := m.AddPortMapping("TCP", 8080, 80, "test", 600)
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
}

func TestMockManagerRemoveError(t *testing.T) {
	injected := errors.New("remove failed")
	m := &MockManager{RemoveErr: injected}

	err := m.RemovePortMapping("TCP", 8080)
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// Verify interface compliance at compile time.
var _ Manager = (*MockManager)(nil)
