// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func marshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func newTestRoot(t *testing.T) *RepositoryRoot {
	t.Helper()
	dir := t.TempDir()
	repos := []Repository{
		{Name: "existing", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/existing.git"}},
	}
	data := marshalJSON(t, repos)
	err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	root, err := RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return root
}

func writePackageYAML(t *testing.T, baseDir, repoName, pkgName, version, content string) {
	t.Helper()
	dir := filepath.Join(baseDir, repoName, PackagesDir, pkgName)
	err := os.MkdirAll(dir, 0750)
	if err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, version+".yaml")
	err = os.WriteFile(path, []byte(content), 0600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func writeTestRepos(t *testing.T, dir string, repos []Repository) {
	t.Helper()
	data := marshalJSON(t, repos)
	if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
