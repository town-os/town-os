package systemcontroller

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestArchiveFormatFromExtension(t *testing.T) {
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

func TestArchiveFormatMagicBytes(t *testing.T) {
	tests := map[string]struct {
		header  []byte
		want    string
		wantErr bool
	}{
		"gzip":  {[]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00}, "tar.gz", false},
		"bzip2": {[]byte{0x42, 0x5a, 0x68, 0x39, 0x31, 0x41}, "tar.bz2", false},
		"xz":    {[]byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00}, "tar.xz", false},
		"unknown": {[]byte{0x50, 0x4b, 0x03, 0x04, 0x00, 0x00}, "", true},
		"short": {[]byte{0x1f}, "", true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, _, err := detectArchiveFormat(bytes.NewReader(tt.header))
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

func TestDownloadCompressProgram(t *testing.T) {
	tests := map[string]struct {
		format       string
		wantProgram  string
		wantType     string
		wantFilename string
	}{
		"tar.gz":  {"tar.gz", "pigz", "application/gzip", "download.tar.gz"},
		"tar.bz2": {"tar.bz2", "lbzip2", "application/x-bzip2", "download.tar.bz2"},
		"tar.xz":  {"tar.xz", "xz", "application/x-xz", "download.tar.xz"},
		"default":  {"", "", "application/gzip", "download.tar.gz"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			program := compressProgramArg(tt.format)
			if program != tt.wantProgram {
				t.Fatalf("program: expected %q, got %q", tt.wantProgram, program)
			}
			contentType := downloadContentType(tt.format)
			if contentType != tt.wantType {
				t.Fatalf("content type: expected %q, got %q", tt.wantType, contentType)
			}
			filename := downloadFilename(tt.format)
			if filename != tt.wantFilename {
				t.Fatalf("filename: expected %q, got %q", tt.wantFilename, filename)
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
func (b *archiveTestBackend) GetPagesStore() *PagesStore                          { return nil }
func (b *archiveTestBackend) GetGitClient() git.Client                            { return nil }
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

	t.Run("error includes URL", func(t *testing.T) {
		dir := t.TempDir()
		err := gitCloneIntoPath(context.Background(), "https://invalid.example.com/nonexistent/repo.git", dir)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "invalid.example.com") {
			t.Fatalf("expected error to include URL, got: %v", err)
		}
	})

	t.Run("successful clone from local bare repo", func(t *testing.T) {
		ctx := context.Background()

		// Create a bare repo with one commit.
		bare := t.TempDir()
		cmd := exec.CommandContext(ctx, "git", "init", "--bare", bare)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("init bare: %v: %s", err, out)
		}

		// Clone, seed a file, push back.
		work := t.TempDir()
		cmd = exec.CommandContext(ctx, "git", "clone", bare, work)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("clone: %v: %s", err, out)
		}
		for _, args := range [][]string{
			{"config", "user.email", "test@test.com"},
			{"config", "user.name", "Test"},
			{"config", "commit.gpgSign", "false"},
		} {
			cmd = exec.CommandContext(ctx, "git", append([]string{"-C", work}, args...)...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("config: %v: %s", err, out)
			}
		}
		if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("world"), 0600); err != nil {
			t.Fatalf("write: %v", err)
		}
		cmd = exec.CommandContext(ctx, "git", "-C", work, "add", ".")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("add: %v: %s", err, out)
		}
		cmd = exec.CommandContext(ctx, "git", "-C", work, "commit", "-m", "init")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("commit: %v: %s", err, out)
		}
		cmd = exec.CommandContext(ctx, "git", "-C", work, "push")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("push: %v: %s", err, out)
		}

		// Clone into a fresh target using gitCloneIntoPath.
		target := filepath.Join(t.TempDir(), "cloned")
		err := gitCloneIntoPath(ctx, bare, target)
		if err != nil {
			t.Fatalf("gitCloneIntoPath: %v", err)
		}

		// Verify the file exists.
		data, err := os.ReadFile(filepath.Join(target, "hello.txt"))
		if err != nil {
			t.Fatalf("expected hello.txt in cloned repo: %v", err)
		}
		if string(data) != "world" {
			t.Fatalf("expected 'world', got %q", data)
		}
	})

	t.Run("cancelled context fails", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		dir := filepath.Join(t.TempDir(), "cloned")
		err := gitCloneIntoPath(ctx, "https://example.com/repo.git", dir)
		if err == nil {
			t.Fatal("expected error for cancelled context")
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

func TestServiceNameFromVolumePath(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"full path":   {"repo/nginx/1.0/data", "town-os-package--repo-nginx-1.0.service"},
		"no vol name": {"repo/nginx/1.0", "town-os-package--repo-nginx-1.0.service"},
		"deep path":   {"myrepo/app/2.5/logs/sub", "town-os-package--myrepo-app-2.5.service"},
		"too short":   {"repo/name", ""},
		"single":      {"repo", ""},
		"empty":       {"", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := serviceNameFromVolumePath(tt.input)
			if got != tt.want {
				t.Fatalf("serviceNameFromVolumePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
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

func TestMatchMagicBytes(t *testing.T) {
	tests := map[string]struct {
		header []byte
		want   string
	}{
		"gzip":      {[]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00}, "tar.gz"},
		"bzip2":     {[]byte{0x42, 0x5a, 0x68, 0x39, 0x31, 0x41}, "tar.bz2"},
		"xz":        {[]byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00}, "tar.xz"},
		"empty":     {[]byte{}, ""},
		"one byte":  {[]byte{0x1f}, ""},
		"random":    {[]byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x00}, ""},
		"text":      {[]byte("Hello!"), ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := matchMagicBytes(tt.header)
			if got != tt.want {
				t.Fatalf("matchMagicBytes(%v) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestDetectArchiveFormatGzip(t *testing.T) {
	// gzip magic: 0x1f 0x8b
	data := append([]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00}, []byte("rest of data")...)
	format, reader, err := detectArchiveFormat(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != "tar.gz" {
		t.Fatalf("expected tar.gz, got %q", format)
	}
	// Verify that the reader replays all original data.
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Fatalf("reader did not replay original data")
	}
}

func TestDetectArchiveFormatBzip2(t *testing.T) {
	data := append([]byte{0x42, 0x5a, 0x68, 0x39, 0x31, 0x41}, []byte("rest of data")...)
	format, _, err := detectArchiveFormat(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != "tar.bz2" {
		t.Fatalf("expected tar.bz2, got %q", format)
	}
}

func TestDetectArchiveFormatXZ(t *testing.T) {
	data := append([]byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00}, []byte("rest of data")...)
	format, _, err := detectArchiveFormat(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if format != "tar.xz" {
		t.Fatalf("expected tar.xz, got %q", format)
	}
}

func TestDetectArchiveFormatInvalid(t *testing.T) {
	_, _, err := detectArchiveFormat(strings.NewReader("this is not an archive"))
	if err == nil {
		t.Fatal("expected error for invalid magic bytes")
	}
	if !errors.Is(err, ErrUnsupportedArchive) {
		t.Fatalf("expected ErrUnsupportedArchive, got: %v", err)
	}
}

func TestDetectArchiveFormatEmpty(t *testing.T) {
	_, _, err := detectArchiveFormat(strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for empty reader")
	}
	if !errors.Is(err, ErrUnsupportedArchive) {
		t.Fatalf("expected ErrUnsupportedArchive, got: %v", err)
	}
}

func TestDetectArchiveFormatOneByte(t *testing.T) {
	_, _, err := detectArchiveFormat(strings.NewReader("x"))
	if err == nil {
		t.Fatal("expected error for single byte")
	}
	if !errors.Is(err, ErrUnsupportedArchive) {
		t.Fatalf("expected ErrUnsupportedArchive, got: %v", err)
	}
}

func TestCompressProgramArg(t *testing.T) {
	tests := map[string]string{
		"tar.gz":  "pigz",
		"tar.bz2": "lbzip2",
		"tar.xz":  "xz",
		"tar":     "",
		"other":   "",
	}

	for format, want := range tests {
		t.Run(format, func(t *testing.T) {
			got := compressProgramArg(format)
			if got != want {
				t.Fatalf("compressProgramArg(%q) = %q, want %q", format, got, want)
			}
		})
	}
}

func TestDownloadContentType(t *testing.T) {
	tests := map[string]string{
		"tar.gz":  "application/gzip",
		"tar.bz2": "application/x-bzip2",
		"tar.xz":  "application/x-xz",
		"":        "application/gzip",
	}

	for format, want := range tests {
		t.Run(format, func(t *testing.T) {
			got := downloadContentType(format)
			if got != want {
				t.Fatalf("downloadContentType(%q) = %q, want %q", format, got, want)
			}
		})
	}
}

func TestDownloadFilenameDefaults(t *testing.T) {
	tests := map[string]string{
		"tar.gz":  "download.tar.gz",
		"tar.bz2": "download.tar.bz2",
		"tar.xz":  "download.tar.xz",
		"":        "download.tar.gz",
	}

	for format, want := range tests {
		t.Run(format, func(t *testing.T) {
			got := downloadFilename("", format)
			if got != want {
				t.Fatalf("downloadFilename(%q, %q) = %q, want %q", "", format, got, want)
			}
		})
	}
}

func TestDownloadFilenameCustom(t *testing.T) {
	tests := map[string]struct {
		name   string
		format string
		want   string
	}{
		"simple name":           {"my-backup", "tar.gz", "my-backup.tar.gz"},
		"name with bz2":        {"my-backup", "tar.bz2", "my-backup.tar.bz2"},
		"name with xz":         {"my-backup", "tar.xz", "my-backup.tar.xz"},
		"name with extension":  {"my-backup.tar.gz", "tar.gz", "my-backup.tar.gz"},
		"name with wrong ext":  {"my-backup.tar.bz2", "tar.gz", "my-backup.tar.gz"},
		"name with tgz":        {"my-backup.tgz", "tar.gz", "my-backup.tar.gz"},
		"path traversal":       {"../../../etc/passwd", "tar.gz", "passwd.tar.gz"},
		"path separator":       {"dir/subdir/file", "tar.gz", "file.tar.gz"},
		"empty after sanitize": {"..", "tar.gz", "download.tar.gz"},
		"only extension":       {".tar.gz", "tar.gz", "download.tar.gz"},
		"control chars":        {"my\x00file\x1f", "tar.gz", "myfile.tar.gz"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := downloadFilename(tt.name, tt.format)
			if got != tt.want {
				t.Fatalf("downloadFilename(%q, %q) = %q, want %q", tt.name, tt.format, got, tt.want)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"simple":         {"backup", "backup"},
		"with extension": {"backup.tar.gz", "backup.tar.gz"},
		"path traversal": {"../../etc/passwd", "passwd"},
		"path sep":       {"a/b/c", "c"},
		"dot":            {".", ""},
		"dotdot":         {"..", ""},
		"slash":          {"/", ""},
		"control chars":  {"abc\x00def\x1f", "abcdef"},
		"delete char":    {"abc\x7fdef", "abcdef"},
		"normal spaces":  {"my file", "my file"},
		"empty":          {"", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.want {
				t.Fatalf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDownloadExtension(t *testing.T) {
	tests := map[string]string{
		"tar.gz":  ".tar.gz",
		"tar.bz2": ".tar.bz2",
		"tar.xz":  ".tar.xz",
		"":        ".tar.gz",
	}

	for format, want := range tests {
		t.Run(format, func(t *testing.T) {
			got := downloadExtension(format)
			if got != want {
				t.Fatalf("downloadExtension(%q) = %q, want %q", format, got, want)
			}
		})
	}
}

func TestValidDownloadFormat(t *testing.T) {
	valid := []string{"tar.gz", "tar.bz2", "tar.xz", ""}
	for _, f := range valid {
		if !validDownloadFormat(f) {
			t.Fatalf("expected %q to be valid", f)
		}
	}

	invalid := []string{"tar", "zip", "rar", "tar.zst"}
	for _, f := range invalid {
		if validDownloadFormat(f) {
			t.Fatalf("expected %q to be invalid", f)
		}
	}
}

func TestValidateTarStreamValid(t *testing.T) {
	// Build a valid tar in memory.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: "hello.txt", Mode: 0644, Size: int64(len("hello world"))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar write header: %v", err)
	}
	if _, err := tw.Write([]byte("hello world")); err != nil {
		t.Fatalf("tar write body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	ch := validateTarStream(context.Background(), &buf)
	for err := range ch {
		if err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	}
}

func TestValidateTarStreamInvalid(t *testing.T) {
	ch := validateTarStream(context.Background(), strings.NewReader("this is definitely not tar data"))
	var gotErr error
	for err := range ch {
		if err != nil {
			gotErr = err
		}
	}
	if gotErr == nil {
		t.Fatal("expected error for invalid tar stream")
	}
	if !errors.Is(gotErr, ErrInvalidTar) {
		t.Fatalf("expected ErrInvalidTar, got: %v", gotErr)
	}
}
