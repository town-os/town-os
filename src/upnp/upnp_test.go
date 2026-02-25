package upnp

import (
	"errors"
	"testing"
)

func TestMockManagerRecordsCalls(t *testing.T) {
	m := &MockManager{}

	err := m.AddPortMapping("TCP", 8080, 80, "test", 600)
	if err != nil {
		t.Fatalf("AddPortMapping: %v", err)
	}
	err = m.RemovePortMapping("TCP", 8080)
	if err != nil {
		t.Fatalf("RemovePortMapping: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}

	if calls[0].Method != "AddPortMapping" {
		t.Fatalf("expected AddPortMapping, got %s", calls[0].Method)
	}
	protocol, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatalf("expected string for protocol, got %T", calls[0].Args[0])
	}
	if protocol != "TCP" {
		t.Fatalf("expected protocol TCP, got %v", protocol)
	}
	extPort, ok := calls[0].Args[1].(uint16)
	if !ok {
		t.Fatalf("expected uint16 for external port, got %T", calls[0].Args[1])
	}
	if extPort != 8080 {
		t.Fatalf("expected external port 8080, got %v", extPort)
	}
	intPort, ok := calls[0].Args[2].(uint16)
	if !ok {
		t.Fatalf("expected uint16 for internal port, got %T", calls[0].Args[2])
	}
	if intPort != 80 {
		t.Fatalf("expected internal port 80, got %v", intPort)
	}
	desc, ok := calls[0].Args[3].(string)
	if !ok {
		t.Fatalf("expected string for description, got %T", calls[0].Args[3])
	}
	if desc != "test" {
		t.Fatalf("expected description test, got %v", desc)
	}
	ttl, ok := calls[0].Args[4].(uint32)
	if !ok {
		t.Fatalf("expected uint32 for ttl, got %T", calls[0].Args[4])
	}
	if ttl != 600 {
		t.Fatalf("expected ttl 600, got %v", ttl)
	}

	if calls[1].Method != "RemovePortMapping" {
		t.Fatalf("expected RemovePortMapping, got %s", calls[1].Method)
	}
	protocol, ok = calls[1].Args[0].(string)
	if !ok {
		t.Fatalf("expected string for protocol, got %T", calls[1].Args[0])
	}
	if protocol != "TCP" {
		t.Fatalf("expected protocol TCP, got %v", protocol)
	}
	extPort, ok = calls[1].Args[1].(uint16)
	if !ok {
		t.Fatalf("expected uint16 for external port, got %T", calls[1].Args[1])
	}
	if extPort != 8080 {
		t.Fatalf("expected external port 8080, got %v", extPort)
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
