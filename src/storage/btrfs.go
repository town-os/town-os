package storage

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

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
	return c.BinPath
}

func (c BtrFSController) IsSubvolume(name string) error {
	out, err := exec.Command(c.binPath(), "subvolume", "show", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("btrfs subvolume show: %w\n%s", err, string(out))
	}
	return nil
}

func (c BtrFSController) SubvolCreate(name string) error {
	out, err := exec.Command(c.binPath(), "subvolume", "create", name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("btrfs subvolume create: %w\n%s", err, string(out))
	}
	return nil
}

func (c BtrFSController) SubvolDelete(name string) error {
	out, err := exec.Command(c.binPath(), "subvolume", "delete", name).CombinedOutput()
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
	args := []string{"subvolume", "snapshot"}
	if readonly {
		args = append(args, "-r")
	}
	args = append(args, src, dst)

	out, err := exec.Command(c.binPath(), args...).CombinedOutput()
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
		if strings.HasPrefix(line, "Name:") {
			info.Name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		} else if strings.HasPrefix(line, "Subvolume ID:") {
			idStr := strings.TrimSpace(strings.TrimPrefix(line, "Subvolume ID:"))
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
	out, err := exec.Command(c.binPath(), "subvolume", "show", name).CombinedOutput()
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
	mnt, err := findMountPoint(name)
	if err != nil {
		return nil, err
	}

	p, err := filepath.Abs(name)
	if err != nil {
		return nil, fmt.Errorf("could not determine absolute path of prefix: %v", err)
	}

	s, err := filepath.Rel(mnt, p)
	if err != nil {
		return nil, fmt.Errorf("could not determine relative path: %v", err)
	}

	if s == "." {
		s = ""
	}

	out, err := exec.Command(c.binPath(), "subvolume", "list", name).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("btrfs subvolume list: %w\n%s", err, string(out))
	}

	return parseSubvolList(string(out), s)
}

func (BtrFSController) SubvolRename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (c BtrFSController) QuotaEnable(path string) error {
	out, err := exec.Command(c.binPath(), "quota", "enable", path).CombinedOutput()
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
		limitArg = fmt.Sprintf("%d", bytes)
	}

	out, err := exec.Command(c.binPath(), "qgroup", "limit", limitArg, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("btrfs qgroup limit: %w\n%s", err, string(out))
	}

	return nil
}

func parseQGroupShow(output string, subvolID uint64) (uint64, error) {
	target := fmt.Sprintf("0/%d", subvolID)
	scanner := bufio.NewScanner(strings.NewReader(output))

	// Skip 2 header lines
	for i := 0; i < 2; i++ {
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
		return 0, nil
	}

	out, err := exec.Command(c.binPath(), "qgroup", "show", "--raw", "-r", path).CombinedOutput()
	if err != nil {
		// Quotas not enabled or other failure — not an error, just no quota
		return 0, nil
	}

	return parseQGroupShow(string(out), id)
}

type BtrFS struct {
	BasePath   string
	BinPath    string
	Controller Controller
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
	if err := ValidateFilesystemName(f.Name); err != nil {
		return err
	}

	// Auto-create intermediate subvolumes for nested paths
	parts := strings.Split(f.Name, "/")
	if len(parts) > 1 {
		for i := 1; i < len(parts); i++ {
			intermediate := filepath.Join(b.BasePath, strings.Join(parts[:i], "/"))
			if err := b.Controller.IsSubvolume(intermediate); err != nil {
				if err := b.Controller.SubvolCreate(intermediate); err != nil {
					return fmt.Errorf("create intermediate subvolume %q: %w", intermediate, err)
				}
			}
		}
	}

	path := filepath.Join(b.BasePath, f.Name)
	if err := b.Controller.SubvolCreate(path); err != nil {
		return err
	}

	if f.Quota > 0 {
		if err := b.Controller.QuotaEnable(path); err != nil {
			return fmt.Errorf("enable quota: %w", err)
		}
		if err := b.Controller.QGroupLimit(path, f.Quota); err != nil {
			return err
		}
	}

	return nil
}

func (b *BtrFS) ModifyFilesystem(name string, f Filesystem) error {
	if err := ValidateFilesystemName(f.Name); err != nil {
		return err
	}

	oldPath := filepath.Join(b.BasePath, name)

	if name != f.Name {
		newPath := filepath.Join(b.BasePath, f.Name)
		if err := b.Controller.SubvolRename(oldPath, newPath); err != nil {
			return fmt.Errorf("rename subvolume: %w", err)
		}
		oldPath = newPath
	}

	if f.Quota > 0 {
		if err := b.Controller.QuotaEnable(oldPath); err != nil {
			return fmt.Errorf("enable quota: %w", err)
		}
		if err := b.Controller.QGroupLimit(oldPath, f.Quota); err != nil {
			return fmt.Errorf("set quota: %w", err)
		}
	} else {
		current, err := b.Controller.QGroupShow(oldPath)
		if err != nil {
			return fmt.Errorf("check quota: %w", err)
		}
		if current > 0 {
			if err := b.Controller.QGroupLimit(oldPath, 0); err != nil {
				return fmt.Errorf("clear quota: %w", err)
			}
		}
	}

	return nil
}

func (b *BtrFS) RemoveFilesystem(name string) error {
	return b.Controller.SubvolDelete(filepath.Join(b.BasePath, name))
}

func (b *BtrFS) ListFilesystems(prefix string) ([]Filesystem, error) {
	info, err := b.Controller.SubvolList(filepath.Join(b.BasePath, prefix))
	if err != nil {
		return nil, err
	}

	fs := []Filesystem{}

	for _, item := range info {
		// filepath.Join strips the trailing slash from the prefix, so
		// SubvolList may return entries that don't actually match.
		// Re-filter against the original prefix to avoid including the
		// parent itself or unrelated volumes that share a name prefix
		// (e.g. "nginx" matching "nginx2").
		if prefix != "" && !strings.HasPrefix(item.Name, prefix) {
			continue
		}

		quota, err := b.Controller.QGroupShow(filepath.Join(b.BasePath, item.Name))
		if err != nil {
			return nil, fmt.Errorf("qgroup show %q: %w", item.Name, err)
		}

		fs = append(fs, Filesystem{Name: item.Name, Quota: quota})
	}

	return fs, nil
}
