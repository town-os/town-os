package storage

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// btrfsTimeout is the default timeout for btrfs CLI operations.
const btrfsTimeout = 30 * time.Second

var ErrMountNotFound = errors.New("mount point not found")

func findMountPoint(path string) (_ string, err error) {
	fp, err := os.Open("/proc/self/mounts")
	if err != nil {
		return "", err
	}
	defer func() {
		err = errors.Join(err, fp.Close())
	}()

	const (
		deviceIdx = 0
		pathIdx   = 1
		typeIdx   = 2
		options   = 3
	)

	var (
		mount   string
		scanner = bufio.NewScanner(fp)
	)

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) <= typeIdx {
			continue // skip malformed lines
		}

		if fields[typeIdx] != "btrfs" {
			continue // skip non-btrfs
		}

		mp := fields[pathIdx]
		if path == mp || strings.HasPrefix(path, mp+"/") {
			mount = mp
		}
	}

	if scanner.Err() != nil {
		return "", scanner.Err()
	}

	if mount == "" {
		return "", ErrMountNotFound
	}

	return mount, nil
}

type BtrFSController struct {
	BinPath string
}

func (c BtrFSController) binPath() string {
	if c.BinPath == "" {
		return "btrfs"
	}
	return filepath.Clean(c.BinPath)
}

func (c BtrFSController) run(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, c.binPath(), args...).CombinedOutput() //nolint:gosec // G204 -- args from trusted internal calls
}

// runWithTimeout executes a btrfs command with the default timeout.
func (c BtrFSController) runWithTimeout(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), btrfsTimeout)
	defer cancel()
	return c.run(ctx, args...)
}

func (c BtrFSController) IsSubvolume(name string) error {
	name = filepath.Clean(name)
	out, err := c.runWithTimeout("subvolume", "show", name)
	if err != nil {
		return fmt.Errorf("btrfs subvolume show: %w\n%s", err, string(out))
	}
	return nil
}

func (c BtrFSController) SubvolCreate(name string) error {
	name = filepath.Clean(name)
	out, err := c.runWithTimeout("subvolume", "create", name)
	if err != nil {
		return fmt.Errorf("btrfs subvolume create: %w\n%s", err, string(out))
	}
	return nil
}

func (c BtrFSController) SubvolDelete(name string) error {
	name = filepath.Clean(name)
	out, err := c.runWithTimeout("subvolume", "delete", name)
	if err != nil {
		return fmt.Errorf("btrfs subvolume delete: %w\n%s", err, string(out))
	}
	return nil
}

func (c BtrFSController) SubvolID(name string) (uint64, error) {
	info, err := c.SubvolInfo(name)
	if err != nil {
		return 0, err
	}
	return info.ID, nil
}

func (c BtrFSController) SubvolSnapshot(dst, src string, readonly bool) error {
	dst = filepath.Clean(dst)
	src = filepath.Clean(src)
	args := []string{"subvolume", "snapshot"}
	if readonly {
		args = append(args, "-r")
	}
	args = append(args, src, dst)

	out, err := c.runWithTimeout(args...)
	if err != nil {
		return fmt.Errorf("btrfs subvolume snapshot: %w\n%s", err, string(out))
	}
	return nil
}

func parseSubvolShow(output string) (SubvolInfo, error) {
	var info SubvolInfo
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if after, ok := strings.CutPrefix(line, "Name:"); ok {
			info.Name = strings.TrimSpace(after)
		} else if after, ok := strings.CutPrefix(line, "Subvolume ID:"); ok {
			idStr := strings.TrimSpace(after)
			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				return SubvolInfo{}, fmt.Errorf("parse subvolume ID %q: %w", idStr, err)
			}
			info.ID = id
		}
	}

	if scanner.Err() != nil {
		return SubvolInfo{}, scanner.Err()
	}

	return info, nil
}

func (c BtrFSController) SubvolInfo(name string) (SubvolInfo, error) {
	name = filepath.Clean(name)
	out, err := c.runWithTimeout("subvolume", "show", name)
	if err != nil {
		return SubvolInfo{}, fmt.Errorf("btrfs subvolume show: %w\n%s", err, string(out))
	}

	return parseSubvolShow(string(out))
}

func parseSubvolList(output string, prefix string) ([]SubvolInfo, error) {
	uniq := map[string]struct{}{}
	var fs []SubvolInfo

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		// Format: ID <id> gen <gen> top level <tl> path <path>
		fields := strings.Fields(scanner.Text())
		if len(fields) < 9 || fields[0] != "ID" {
			continue
		}

		id, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		subvolPath := strings.Join(fields[8:], " ")
		if prefix == "" || strings.HasPrefix(subvolPath, prefix) {
			if _, ok := uniq[subvolPath]; !ok {
				fs = append(fs, SubvolInfo{Name: subvolPath, ID: id})
				uniq[subvolPath] = struct{}{}
			}
		}
	}

	if scanner.Err() != nil {
		return nil, scanner.Err()
	}

	return fs, nil
}

func (c BtrFSController) SubvolList(name string) ([]SubvolInfo, error) {
	name = filepath.Clean(name)
	mnt, err := findMountPoint(name)
	if err != nil {
		return nil, err
	}

	p, err := filepath.Abs(name)
	if err != nil {
		return nil, fmt.Errorf("could not determine absolute path of prefix: %w", err)
	}

	s, err := filepath.Rel(mnt, p)
	if err != nil {
		return nil, fmt.Errorf("could not determine relative path: %w", err)
	}

	if s == "." {
		s = ""
	}

	out, err := c.runWithTimeout("subvolume", "list", name)
	if err != nil {
		return nil, fmt.Errorf("btrfs subvolume list: %w\n%s", err, string(out))
	}

	return parseSubvolList(string(out), s)
}

func (BtrFSController) SubvolRename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (c BtrFSController) QuotaEnable(path string) error {
	path = filepath.Clean(path)
	out, err := c.runWithTimeout("quota", "enable", path)
	if err != nil {
		return fmt.Errorf("btrfs quota enable: %w\n%s", err, string(out))
	}

	return nil
}

func (c BtrFSController) QGroupLimit(path string, bytes uint64) error {
	var limitArg string
	if bytes == 0 {
		limitArg = "none"
	} else {
		limitArg = strconv.FormatUint(bytes, 10)
	}

	path = filepath.Clean(path)
	out, err := c.runWithTimeout("qgroup", "limit", limitArg, path)
	if err != nil {
		return fmt.Errorf("btrfs qgroup limit: %w\n%s", err, string(out))
	}

	return nil
}

func parseQGroupShow(output string, subvolID uint64) (uint64, error) {
	target := fmt.Sprintf("0/%d", subvolID)
	scanner := bufio.NewScanner(strings.NewReader(output))

	// Skip 2 header lines
	for range 2 {
		if !scanner.Scan() {
			return 0, nil
		}
	}

	// Search data lines for matching qgroup: qgroupid rfer excl max_rfer ...
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}

		if fields[0] != target {
			continue
		}

		maxRfer := fields[3]
		if maxRfer == "none" {
			return 0, nil
		}

		val, err := strconv.ParseUint(maxRfer, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse max_rfer %q: %w", maxRfer, err)
		}

		return val, nil
	}

	return 0, nil
}

func (c BtrFSController) QGroupShow(path string) (uint64, error) {
	id, err := c.SubvolID(path)
	if err != nil {
		return 0, err
	}

	path = filepath.Clean(path)
	out, err := c.runWithTimeout("qgroup", "show", "--raw", "-r", path)
	if err != nil {
		// Quotas not enabled or other failure — not an error, just no quota
		return 0, nil //nolint:nilerr // qgroup failure means no quota
	}

	return parseQGroupShow(string(out), id)
}

type BtrFS struct {
	BasePath          string
	BinPath           string
	Controller        Controller
	DiskUsageOverride *DiskUsage
}

func InitBtrFS(basePath string) *BtrFS {
	return InitBtrFSFromController(basePath, BtrFSController{BinPath: "btrfs"})
}

func InitBtrFSMock() *BtrFS {
	return InitBtrFSFromController("", InitBtrFSMockController())
}

func InitBtrFSFromController(basePath string, c Controller) *BtrFS {
	return &BtrFS{
		BasePath:   basePath,
		BinPath:    "btrfs",
		Controller: c,
	}
}

func (b *BtrFS) CreateFilesystem(f Filesystem) error {
	err := ValidateFilesystemName(f.Name)
	if err != nil {
		return err
	}

	// Auto-create intermediate subvolumes for nested paths
	parts := strings.Split(f.Name, "/")
	if len(parts) > 1 {
		for i := 1; i < len(parts); i++ {
			intermediate := filepath.Join(b.BasePath, strings.Join(parts[:i], "/"))
			err := b.Controller.IsSubvolume(intermediate)
			if err != nil {
				err := b.Controller.SubvolCreate(intermediate)
				if err != nil {
					return fmt.Errorf("create intermediate subvolume %q: %w", intermediate, err)
				}
			}
		}
	}

	path := filepath.Join(b.BasePath, f.Name)
	err = b.Controller.SubvolCreate(path)
	if err != nil {
		return err
	}

	if f.Quota > 0 {
		err = b.Controller.QuotaEnable(path)
		if err != nil {
			return fmt.Errorf("enable quota: %w", err)
		}
		err = b.Controller.QGroupLimit(path, f.Quota)
		if err != nil {
			return err
		}
	}

	return nil
}

func (b *BtrFS) ModifyFilesystem(name string, f Filesystem) error {
	err := ValidateFilesystemName(f.Name)
	if err != nil {
		return err
	}

	oldPath := filepath.Join(b.BasePath, name)

	if name != f.Name {
		newPath := filepath.Join(b.BasePath, f.Name)
		err := b.Controller.SubvolRename(oldPath, newPath)
		if err != nil {
			return fmt.Errorf("rename subvolume: %w", err)
		}
		oldPath = newPath
	}

	if f.Quota > 0 {
		err := b.Controller.QuotaEnable(oldPath)
		if err != nil {
			return fmt.Errorf("enable quota: %w", err)
		}
		err = b.Controller.QGroupLimit(oldPath, f.Quota)
		if err != nil {
			return fmt.Errorf("set quota: %w", err)
		}
	} else {
		current, err := b.Controller.QGroupShow(oldPath)
		if err != nil {
			return fmt.Errorf("check quota: %w", err)
		}
		if current > 0 {
			err := b.Controller.QGroupLimit(oldPath, 0)
			if err != nil {
				return fmt.Errorf("clear quota: %w", err)
			}
		}
	}

	return nil
}

func (b *BtrFS) RemoveFilesystem(name string) error {
	return b.Controller.SubvolDelete(filepath.Join(b.BasePath, name))
}

func (b *BtrFS) RenameFilesystem(oldName, newName string) error {
	err := ValidateFilesystemName(oldName)
	if err != nil {
		return err
	}
	err = ValidateFilesystemName(newName)
	if err != nil {
		return err
	}

	return b.Controller.SubvolRename(filepath.Join(b.BasePath, oldName), filepath.Join(b.BasePath, newName))
}

func (b *BtrFS) SnapshotFilesystem(src, dst string) error {
	err := ValidateFilesystemName(src)
	if err != nil {
		return err
	}
	err = ValidateFilesystemName(dst)
	if err != nil {
		return err
	}

	return b.Controller.SubvolSnapshot(filepath.Join(b.BasePath, dst), filepath.Join(b.BasePath, src), false)
}

func (b *BtrFS) ListFilesystems(prefix string) ([]Filesystem, error) {
	// Always list from the base path so that btrfs subvolume list runs
	// against the filesystem root. Passing a nested subvolume path can
	// produce path-relative names that don't match the expected prefix.
	info, err := b.Controller.SubvolList(b.BasePath)
	if err != nil {
		if errors.Is(err, ErrMountNotFound) {
			slog.Warn("listing filesystems: no btrfs mount point found", "path", b.BasePath)
			return []Filesystem{}, nil
		}
		return nil, err
	}

	fs := []Filesystem{}

	for _, item := range info {
		if prefix != "" && !strings.HasPrefix(item.Name, prefix) {
			continue
		}

		quota, err := b.Controller.QGroupShow(filepath.Join(b.BasePath, item.Name))
		if err != nil {
			// Subvolume may have been deleted between the list and the
			// quota query (concurrent operations). Skip only when the
			// error indicates the path no longer exists.
			errMsg := err.Error()
			if strings.Contains(errMsg, "No such file or directory") || strings.Contains(errMsg, "not a subvolume") {
				slog.Debug("listing filesystems: skipping deleted subvolume", "name", item.Name, "error", err)
				continue
			}
			return nil, fmt.Errorf("qgroup show %q: %w", item.Name, err)
		}

		fs = append(fs, Filesystem{Name: item.Name, Quota: quota})
	}

	return fs, nil
}

func (b *BtrFS) DiskUsage() (_ DiskUsage, err error) {
	if b.DiskUsageOverride != nil {
		return *b.DiskUsageOverride, nil
	}
	var stat syscall.Statfs_t
	err = syscall.Statfs(b.BasePath, &stat)
	if err != nil {
		return DiskUsage{}, fmt.Errorf("statfs %s: %w", b.BasePath, err)
	}
	if stat.Bsize < 0 {
		return DiskUsage{}, fmt.Errorf("statfs %s: negative block size %d", b.BasePath, stat.Bsize)
	}
	bsize := uint64(stat.Bsize)
	total := stat.Blocks * bsize
	available := stat.Bavail * bsize
	return DiskUsage{Total: total, Used: total - available, Available: available}, nil
}
