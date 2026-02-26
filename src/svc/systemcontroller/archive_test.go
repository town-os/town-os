package systemcontroller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestArchiveFormat(t *testing.T) {
	tests := map[string]struct {
		filename string
		want     string
		wantErr  bool
	}{
		"tar.gz":      {"data.tar.gz", "tar.gz", false},
		"tgz":         {"data.tgz", "tar.gz", false},
		"tar.bz2":     {"data.tar.bz2", "tar.bz2", false},
		"tbz2":        {"data.tbz2", "tar.bz2", false},
		"tar.xz":      {"data.tar.xz", "tar.xz", false},
		"txz":         {"data.txz", "tar.xz", false},
		"tar":         {"data.tar", "tar", false},
		"zip":         {"data.zip", "", true},
		"7z":          {"data.7z", "", true},
		"uppercase":   {"DATA.TAR.GZ", "tar.gz", false},
		"mixed case":  {"Data.Tar.Bz2", "tar.bz2", false},
		"unsupported": {"data.rar", "", true},
		"no ext":      {"data", "", true},
		"empty":       {"", "", true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := archiveFormat(tt.filename)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

// archiveTestBackend is a minimal systemControllerBackend for archive tests.
type archiveTestBackend struct {
	settingsMgr account.SettingsManager
	btrfsBase   string
	st          storage.Storage
}

func (b *archiveTestBackend) GetStorage() storage.Storage                         { return b.st }
func (b *archiveTestBackend) GetRepositoryRoot() *packages.RepositoryRoot         { return nil }
func (b *archiveTestBackend) GetInstaller() packages.Installer                    { return nil }
func (b *archiveTestBackend) GetSystemdManager() systemd.Manager                  { return nil }
func (b *archiveTestBackend) GetAccountManager() account.Manager                  { return nil }
func (b *archiveTestBackend) GetSessionManager() account.SessionManager           { return nil }
func (b *archiveTestBackend) GetAuditManager() account.AuditManager               { return nil }
func (b *archiveTestBackend) GetSettingsManager() account.SettingsManager          { return b.settingsMgr }
func (b *archiveTestBackend) GetAllowedHosts() []string                           { return nil }
func (b *archiveTestBackend) GetDefaultRepoCredentials() (string, string)         { return "", "" }
func (b *archiveTestBackend) GetBtrfsBasePath() string                            { return b.btrfsBase }
func (b *archiveTestBackend) GetNetworkControllerBinPath() string                 { return "" }
func (b *archiveTestBackend) GetNetworkStatePath() string                         { return "" }
func (b *archiveTestBackend) GetNetworkMode() string                              { return "" }
func (b *archiveTestBackend) GetExternalIP() string                               { return "" }
func (b *archiveTestBackend) GetInternalIP() string                               { return "" }

type testSettingsManager struct {
	values map[string]string
}

func (m *testSettingsManager) Get(key string) (string, error) {
	v, ok := m.values[key]
	if !ok {
		return "", fmt.Errorf("setting %q not found", key)
	}
	return v, nil
}

func (m *testSettingsManager) Set(key, value string) error {
	m.values[key] = value
	return nil
}

func (m *testSettingsManager) List() (map[string]string, error) {
	return m.values, nil
}

func TestMaxArchiveSizeDefault(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &archiveTestBackend{}}
	got := s.maxArchiveSize()
	if got != DefaultMaxArchiveSize {
		t.Fatalf("expected %d, got %d", DefaultMaxArchiveSize, got)
	}
}

func TestMaxArchiveSizeFromSettings(t *testing.T) {
	mgr := &testSettingsManager{values: map[string]string{
		"max_archive_size": "104857600", // 100 MB
	}}
	s := &SystemControllerHandlers{Controller: &archiveTestBackend{settingsMgr: mgr}}
	got := s.maxArchiveSize()
	if got != 104857600 {
		t.Fatalf("expected 104857600, got %d", got)
	}
}

func TestArchiveUnpackTimeoutDefault(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &archiveTestBackend{}}
	got := s.archiveUnpackTimeout()
	expected := time.Duration(DefaultUnpackTimeout) * time.Second
	if got != expected {
		t.Fatalf("expected %v, got %v", expected, got)
	}
}

func TestArchiveUnpackTimeoutFromSettings(t *testing.T) {
	mgr := &testSettingsManager{values: map[string]string{
		"archive_unpack_timeout": "300",
	}}
	s := &SystemControllerHandlers{Controller: &archiveTestBackend{settingsMgr: mgr}}
	got := s.archiveUnpackTimeout()
	expected := 300 * time.Second
	if got != expected {
		t.Fatalf("expected %v, got %v", expected, got)
	}
}

func TestValidateUnpackedPaths(t *testing.T) {
	t.Run("empty directory passes", func(t *testing.T) {
		dir := t.TempDir()
		if err := validateUnpackedPaths(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGitCloneIntoPath(t *testing.T) {
	t.Run("invalid URL fails gracefully", func(t *testing.T) {
		dir := t.TempDir()
		err := gitCloneIntoPath(context.Background(), "https://invalid.example.com/nonexistent/repo.git", dir)
		if err == nil {
			t.Fatal("expected error for invalid git URL")
		}
	})
}

func TestIsReservedFilesystemIncludesArchives(t *testing.T) {
	if !isReservedFilesystem(ArchivesSubvolume) {
		t.Fatal("expected archives to be reserved")
	}
	if !isReservedFilesystem("archives/staging-123") {
		t.Fatal("expected archives/staging-123 to be reserved")
	}
}

func TestClassifyFilesystemSkipsArchives(t *testing.T) {
	state, _ := classifyFilesystem(ArchivesSubvolume)
	if state != "" {
		t.Fatalf("expected empty state for archives root, got %q", state)
	}
	state, _ = classifyFilesystem("archives/staging-123")
	if state != "" {
		t.Fatalf("expected empty state for archives child, got %q", state)
	}
}
