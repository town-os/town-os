package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// --- SaveLastResponses / LoadLastResponses / ClearLastResponses ---

func TestSaveAndLoadLastResponses(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	resp := Responses{"key1": "val1", "key2": "val2"}
	if err := m.SaveLastResponses("repo", "pkg", resp); err != nil {
		t.Fatalf("SaveLastResponses: %v", err)
	}

	got, err := m.LoadLastResponses("repo", "pkg")
	if err != nil {
		t.Fatalf("LoadLastResponses: %v", err)
	}
	if got["key1"] != "val1" || got["key2"] != "val2" {
		t.Fatalf("expected key1=val1 key2=val2, got %v", got)
	}
}

func TestSaveLastResponsesOverwrites(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	if err := m.SaveLastResponses("repo", "pkg", Responses{"k": "old"}); err != nil {
		t.Fatalf("SaveLastResponses first: %v", err)
	}
	if err := m.SaveLastResponses("repo", "pkg", Responses{"k": "new"}); err != nil {
		t.Fatalf("SaveLastResponses second: %v", err)
	}

	got, err := m.LoadLastResponses("repo", "pkg")
	if err != nil {
		t.Fatalf("LoadLastResponses: %v", err)
	}
	if got["k"] != "new" {
		t.Fatalf("expected k=new, got %v", got)
	}
}

func TestLoadLastResponsesNotFound(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	_, err := m.LoadLastResponses("repo", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing last responses")
	}
}

func TestClearLastResponses(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	if err := m.SaveLastResponses("repo", "pkg", Responses{"k": "v"}); err != nil {
		t.Fatalf("SaveLastResponses: %v", err)
	}

	if err := m.ClearLastResponses("repo", "pkg"); err != nil {
		t.Fatalf("ClearLastResponses: %v", err)
	}

	_, err := m.LoadLastResponses("repo", "pkg")
	if err == nil {
		t.Fatal("expected error after clearing last responses")
	}
}

func TestClearLastResponsesIdempotent(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Clearing non-existent responses should not error.
	if err := m.ClearLastResponses("repo", "pkg"); err != nil {
		t.Fatalf("ClearLastResponses on non-existent: %v", err)
	}
}

// --- SaveChildren / LoadChildren ---

func TestSaveAndLoadChildren(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	children := []string{"child-a", "child-b", "child-c"}
	if err := m.SaveChildren("repo", "parent", children); err != nil {
		t.Fatalf("SaveChildren: %v", err)
	}

	got, err := m.LoadChildren("repo", "parent")
	if err != nil {
		t.Fatalf("LoadChildren: %v", err)
	}
	if len(got) != 3 || got[0] != "child-a" || got[1] != "child-b" || got[2] != "child-c" {
		t.Fatalf("unexpected children: %v", got)
	}
}

func TestSaveChildrenOverwrites(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	if err := m.SaveChildren("repo", "parent", []string{"old"}); err != nil {
		t.Fatalf("SaveChildren first: %v", err)
	}
	if err := m.SaveChildren("repo", "parent", []string{"new-a", "new-b"}); err != nil {
		t.Fatalf("SaveChildren second: %v", err)
	}

	got, err := m.LoadChildren("repo", "parent")
	if err != nil {
		t.Fatalf("LoadChildren: %v", err)
	}
	if len(got) != 2 || got[0] != "new-a" || got[1] != "new-b" {
		t.Fatalf("expected new-a, new-b, got %v", got)
	}
}

func TestLoadChildrenNotFound(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	got, err := m.LoadChildren("repo", "nonexistent")
	if err != nil {
		t.Fatalf("LoadChildren: %v", err)
	}
	// Missing children file returns nil (no error).
	if got != nil {
		t.Fatalf("expected nil for missing children, got %v", got)
	}
}

func TestSaveChildrenEmptySlice(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	if err := m.SaveChildren("repo", "parent", []string{}); err != nil {
		t.Fatalf("SaveChildren: %v", err)
	}

	got, err := m.LoadChildren("repo", "parent")
	if err != nil {
		t.Fatalf("LoadChildren: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

// --- atomicWriteJSON tests ---

func TestAtomicWriteJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	data := map[string]string{"hello": "world"}
	if err := atomicWriteJSON(path, data); err != nil {
		t.Fatalf("atomicWriteJSON: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["hello"] != "world" {
		t.Fatalf("expected hello=world, got %v", got)
	}
}

func TestAtomicWriteJSONOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := atomicWriteJSON(path, map[string]string{"k": "old"}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := atomicWriteJSON(path, map[string]string{"k": "new"}); err != nil {
		t.Fatalf("second write: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["k"] != "new" {
		t.Fatalf("expected k=new, got %v", got)
	}
}

func TestAtomicWriteJSONNoPartialWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	// Write initial value.
	if err := atomicWriteJSON(path, "initial"); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// Verify initial value exists.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got string
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != "initial" {
		t.Fatalf("expected 'initial', got %q", got)
	}
}

// --- lockDir / Unlock tests ---

func TestLockDirAndUnlock(t *testing.T) {
	dir := t.TempDir()

	lock, err := lockDir(dir)
	if err != nil {
		t.Fatalf("lockDir: %v", err)
	}

	// Lock file should exist.
	lockPath := filepath.Join(dir, ".lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file to exist: %v", err)
	}

	if err := lock.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

func TestLockDirConcurrency(t *testing.T) {
	dir := t.TempDir()
	counter := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	iterations := 20

	for range iterations {
		wg.Go(func() {
			lock, err := lockDir(dir)
			if err != nil {
				t.Errorf("lockDir: %v", err)
				return
			}
			defer func() {
				if err := lock.Unlock(); err != nil {
					t.Errorf("Unlock: %v", err)
				}
			}()

			// Critical section: increment counter under file lock.
			mu.Lock()
			counter++
			mu.Unlock()
		})
	}

	wg.Wait()

	if counter != iterations {
		t.Fatalf("expected counter %d, got %d", iterations, counter)
	}
}

func TestLockDirReentrant(t *testing.T) {
	dir := t.TempDir()

	lock1, err := lockDir(dir)
	if err != nil {
		t.Fatalf("lockDir first: %v", err)
	}

	if err := lock1.Unlock(); err != nil {
		t.Fatalf("Unlock first: %v", err)
	}

	// Re-acquiring should succeed after unlock.
	lock2, err := lockDir(dir)
	if err != nil {
		t.Fatalf("lockDir second: %v", err)
	}

	if err := lock2.Unlock(); err != nil {
		t.Fatalf("Unlock second: %v", err)
	}
}

// --- SaveResponses (with file lock) tests ---

func TestSaveResponsesCreatesFile(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	resp := Responses{"key": "value"}
	if err := m.SaveResponses("repo", "pkg", "1.0", resp); err != nil {
		t.Fatalf("SaveResponses: %v", err)
	}

	// Verify the file was written.
	path := filepath.Join(dir, ResponsesDir, "repo", "pkg", "1.0.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected response file to exist: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got Responses
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["key"] != "value" {
		t.Fatalf("expected key=value, got %v", got)
	}
}

func TestSaveResponsesOverwrites(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	if err := m.SaveResponses("repo", "pkg", "1.0", Responses{"k": "old"}); err != nil {
		t.Fatalf("SaveResponses first: %v", err)
	}
	if err := m.SaveResponses("repo", "pkg", "1.0", Responses{"k": "new"}); err != nil {
		t.Fatalf("SaveResponses second: %v", err)
	}

	path := filepath.Join(dir, ResponsesDir, "repo", "pkg", "1.0.json")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got Responses
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got["k"] != "new" {
		t.Fatalf("expected k=new, got %v", got)
	}
}

// --- ListInstalled tests ---

func TestListInstalledEmpty(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	items, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if items != nil {
		t.Fatalf("expected nil for empty installed dir, got %v", items)
	}
}

func TestListInstalledSorted(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Create two packages in different repos.
	for _, r := range []struct {
		repo, name, version string
	}{
		{"zoo", "app", "1.0"},
		{"alpha", "tool", "2.0"},
		{"alpha", "tool", "1.0"},
	} {
		pkgDir := filepath.Join(dir, r.repo, PackagesDir, r.name)
		if err := os.MkdirAll(pkgDir, 0750); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		yamlFile := filepath.Join(pkgDir, r.version+".yaml")
		if err := os.WriteFile(yamlFile, []byte("image: test\n"), 0600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := m.Install(r.repo, r.name, r.version, Responses{}); err != nil {
			t.Fatalf("Install: %v", err)
		}
	}

	items, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Should be sorted: alpha/tool@1.0, alpha/tool@2.0, zoo/app@1.0
	if items[0] != "alpha/tool@1.0" {
		t.Fatalf("expected first item alpha/tool@1.0, got %s", items[0])
	}
	if items[1] != "alpha/tool@2.0" {
		t.Fatalf("expected second item alpha/tool@2.0, got %s", items[1])
	}
	if items[2] != "zoo/app@1.0" {
		t.Fatalf("expected third item zoo/app@1.0, got %s", items[2])
	}
}

// --- SetDisabled / IsDisabled tests ---

func TestSetDisabledAndIsDisabled(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	disabled, err := m.IsDisabled("repo", "pkg")
	if err != nil {
		t.Fatalf("IsDisabled initial: %v", err)
	}
	if disabled {
		t.Fatal("expected not disabled initially")
	}

	if err := m.SetDisabled("repo", "pkg", true); err != nil {
		t.Fatalf("SetDisabled true: %v", err)
	}

	disabled, err = m.IsDisabled("repo", "pkg")
	if err != nil {
		t.Fatalf("IsDisabled after set: %v", err)
	}
	if !disabled {
		t.Fatal("expected disabled after SetDisabled(true)")
	}

	if err := m.SetDisabled("repo", "pkg", false); err != nil {
		t.Fatalf("SetDisabled false: %v", err)
	}

	disabled, err = m.IsDisabled("repo", "pkg")
	if err != nil {
		t.Fatalf("IsDisabled after unset: %v", err)
	}
	if disabled {
		t.Fatal("expected not disabled after SetDisabled(false)")
	}
}

func TestSetDisabledIdempotent(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Disabling twice should not error.
	if err := m.SetDisabled("repo", "pkg", false); err != nil {
		t.Fatalf("SetDisabled false on non-existent: %v", err)
	}
}

// --- SaveResponses concurrent test ---

func TestSaveResponsesConcurrent(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	const goroutines = 10
	var wg sync.WaitGroup

	for i := range goroutines {
		version := fmt.Sprintf("v%d", i)
		wg.Go(func() {
			resp := Responses{"key": version}
			if err := m.SaveResponses("repo", "pkg", version, resp); err != nil {
				t.Errorf("SaveResponses(%s): %v", version, err)
			}
		})
	}

	wg.Wait()

	// Verify all responses are readable and correct.
	for i := range goroutines {
		version := fmt.Sprintf("v%d", i)
		got, err := m.GetResponses("repo", "pkg", version)
		if err != nil {
			t.Fatalf("GetResponses(%s): %v", version, err)
		}
		if got["key"] != version {
			t.Fatalf("expected key=%s, got %v", version, got)
		}
	}
}

// --- Install already installed test ---

func TestInstallAlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Create the source YAML file that Install expects.
	srcDir := filepath.Join(dir, "repo", PackagesDir, "pkg")
	if err := os.MkdirAll(srcDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	srcFile := filepath.Join(srcDir, "1.0.yaml")
	if err := os.WriteFile(srcFile, []byte("image: test\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// First install should succeed.
	if err := m.Install("repo", "pkg", "1.0", Responses{}); err != nil {
		t.Fatalf("Install first: %v", err)
	}

	// Second install of the same package should return ErrAlreadyInstalled.
	err := m.Install("repo", "pkg", "1.0", Responses{})
	if err == nil {
		t.Fatal("expected error for already installed package")
	}
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("expected ErrAlreadyInstalled, got %v", err)
	}
}

// --- Uninstall cleans up directories test ---

func TestUninstallCleansUpDirectories(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Create source YAML file.
	srcDir := filepath.Join(dir, "repo", PackagesDir, "pkg")
	if err := os.MkdirAll(srcDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	srcFile := filepath.Join(srcDir, "1.0.yaml")
	if err := os.WriteFile(srcFile, []byte("image: test\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Install the package.
	if err := m.Install("repo", "pkg", "1.0", Responses{"k": "v"}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify installed directory and responses directory exist.
	installedDir := filepath.Join(dir, InstalledDir, "repo", "pkg")
	if _, err := os.Stat(installedDir); err != nil {
		t.Fatalf("expected installed directory to exist: %v", err)
	}
	responsesDir := filepath.Join(dir, ResponsesDir, "repo", "pkg")
	if _, err := os.Stat(responsesDir); err != nil {
		t.Fatalf("expected responses directory to exist: %v", err)
	}

	// Uninstall the package.
	if err := m.Uninstall("repo", "pkg", "1.0"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	// Verify the installed directory was cleaned up.
	if _, err := os.Stat(installedDir); !os.IsNotExist(err) {
		t.Fatalf("expected installed directory to be removed, got err: %v", err)
	}

	// Verify the responses directory was cleaned up.
	if _, err := os.Stat(responsesDir); !os.IsNotExist(err) {
		t.Fatalf("expected responses directory to be removed, got err: %v", err)
	}
}

// --- GetResponses round-trip test ---

func TestGetResponsesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	expected := Responses{
		"host":     "localhost",
		"port":     "8080",
		"password": "secret123",
		"empty":    "",
	}

	if err := m.SaveResponses("repo", "myapp", "2.5", expected); err != nil {
		t.Fatalf("SaveResponses: %v", err)
	}

	got, err := m.GetResponses("repo", "myapp", "2.5")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}

	if len(got) != len(expected) {
		t.Fatalf("expected %d responses, got %d", len(expected), len(got))
	}

	for k, v := range expected {
		if got[k] != v {
			t.Fatalf("expected %s=%q, got %q", k, v, got[k])
		}
	}
}

// --- atomicWriteJSON concurrent test ---

func TestAtomicWriteJSONConcurrent(t *testing.T) {
	dir := t.TempDir()

	const goroutines = 10
	var wg sync.WaitGroup

	for i := range goroutines {
		fileName := fmt.Sprintf("file-%d.json", i)
		wg.Go(func() {
			path := filepath.Join(dir, fileName)
			data := map[string]int{"index": i}
			if err := atomicWriteJSON(path, data); err != nil {
				t.Errorf("atomicWriteJSON(%s): %v", fileName, err)
			}
		})
	}

	wg.Wait()

	// Verify all files exist and contain valid JSON with the correct value.
	for i := range goroutines {
		fileName := fmt.Sprintf("file-%d.json", i)
		path := filepath.Join(dir, fileName)

		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", fileName, err)
		}

		var got map[string]int
		if err := json.Unmarshal(content, &got); err != nil {
			t.Fatalf("Unmarshal(%s): %v", fileName, err)
		}

		if got["index"] != i {
			t.Fatalf("expected index=%d in %s, got %d", i, fileName, got["index"])
		}
	}
}
