// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"errors"
	"sync"
	"testing"
)

func TestMockGitClonerClone(t *testing.T) {
	m := &MockGitCloner{}
	if err := m.Clone("/target", "https://example.com/repo.git", "main"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "Clone" {
		t.Fatalf("expected Clone, got %q", calls[0].Method)
	}
	if calls[0].Args[0] != "/target" {
		t.Fatalf("expected /target, got %v", calls[0].Args[0])
	}
	if calls[0].Args[1] != "https://example.com/repo.git" {
		t.Fatalf("expected repo URL, got %v", calls[0].Args[1])
	}
	if calls[0].Args[2] != "main" {
		t.Fatalf("expected main, got %v", calls[0].Args[2])
	}
}

func TestMockGitClonerUpdate(t *testing.T) {
	m := &MockGitCloner{}
	if err := m.Update("/target", "develop"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "Update" {
		t.Fatalf("expected Update, got %q", calls[0].Method)
	}
	if calls[0].Args[0] != "/target" {
		t.Fatalf("expected /target, got %v", calls[0].Args[0])
	}
	if calls[0].Args[1] != "develop" {
		t.Fatalf("expected develop, got %v", calls[0].Args[1])
	}
}

func TestMockGitClonerCloneError(t *testing.T) {
	injected := errors.New("clone failed")
	m := &MockGitCloner{CloneErr: injected}
	err := m.Clone("/target", "https://example.com/repo.git", "main")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
	if len(m.GetCalls()) != 1 {
		t.Fatal("expected call to be recorded even on error")
	}
}

func TestMockGitClonerUpdateError(t *testing.T) {
	injected := errors.New("update failed")
	m := &MockGitCloner{UpdateErr: injected}
	err := m.Update("/target", "main")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
	if len(m.GetCalls()) != 1 {
		t.Fatal("expected call to be recorded even on error")
	}
}

func TestMockGitClonerGetCallsReturns(t *testing.T) {
	m := &MockGitCloner{}
	if err := m.Clone("/a", "url-a", "main"); err != nil {
		t.Fatal(err)
	}
	if err := m.Update("/b", "develop"); err != nil {
		t.Fatal(err)
	}

	calls := m.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Method != "Clone" {
		t.Fatalf("expected Clone first, got %q", calls[0].Method)
	}
	if calls[1].Method != "Update" {
		t.Fatalf("expected Update second, got %q", calls[1].Method)
	}

	// Returned slice should be a copy.
	calls[0].Method = "modified"
	original := m.GetCalls()
	if original[0].Method != "Clone" {
		t.Fatal("GetCalls did not return a copy")
	}
}

func TestMockGitClonerConcurrency(t *testing.T) {
	m := &MockGitCloner{}
	var wg sync.WaitGroup
	n := 50
	wg.Add(n)
	cloneErrs := make([]error, n)
	for i := range n {
		go func() {
			defer wg.Done()
			cloneErrs[i] = m.Clone("/target", "url", "main")
		}()
	}
	wg.Wait()

	for _, cloneErr := range cloneErrs {
		if cloneErr != nil {
			t.Fatal(cloneErr)
		}
	}

	calls := m.GetCalls()
	if len(calls) != n {
		t.Fatalf("expected %d calls, got %d", n, len(calls))
	}
}
