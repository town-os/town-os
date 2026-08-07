package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"github.com/labstack/echo/v5"
)

const (
	// VMImagesSubvolume is the btrfs subvolume used to store cached VM disk images.
	VMImagesSubvolume = "vm-images"
)

var (
	// ErrVMImageNotFound indicates a requested VM image does not exist.
	ErrVMImageNotFound = errors.New("vm image not found")
	// ErrVMImageConvert indicates qemu-img conversion failed.
	ErrVMImageConvert = errors.New("vm image conversion failed")
)

// VMImageInfo describes a cached VM image file.
type VMImageInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// listVMImages returns a list of cached VM image files in the vm-images
// subvolume.
//
// GET /vm-images
// Returns: []VMImageInfo.
func (s *SystemControllerHandlers) listVMImages(c *echo.Context) error {
	basePath := s.Controller.GetBtrfsBasePath()
	if basePath == "" {
		return errors.New("storage not configured")
	}

	dir := filepath.Join(basePath, VMImagesSubvolume)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return c.JSON(200, []VMImageInfo{})
		}
		return err
	}

	images := make([]VMImageInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		images = append(images, VMImageInfo{
			Name: entry.Name(),
			Size: info.Size(),
		})
	}

	return c.JSON(200, images)
}

// UploadVMImageRequest is the JSON body for uploading a VM image from a URL.
type UploadVMImageRequest struct {
	// URL is the remote URL to download the VM image from.
	URL string `json:"url"`
	// Name is the desired filename for the cached image. Derived from URL if empty.
	Name string `json:"name"`
}

// uploadVMImage downloads a VM image from a URL and caches it to the
// vm-images subvolume. The image is automatically converted to raw format
// using qemu-img.
//
// POST /vm-images/upload
// Body: UploadVMImageRequest.
func (s *SystemControllerHandlers) uploadVMImage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := UploadVMImageRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.URL == "" {
		return errors.New("url is required")
	}

	basePath := s.Controller.GetBtrfsBasePath()
	if basePath == "" {
		return errors.New("storage not configured")
	}

	st := s.Controller.GetStorage()
	if st != nil {
		if err := st.CreateFilesystem(storage.Filesystem{Name: VMImagesSubvolume}); err != nil {
			slog.Debug(fmt.Sprintf("create vm-images subvolume: %v", err))
		}
	}

	name := req.Name
	if name == "" {
		// Derive name from URL.
		parts := strings.Split(req.URL, "/")
		name = parts[len(parts)-1]
		if name == "" {
			name = "vm-image"
		}
	}

	// Sanitize the name.
	name = filepath.Base(name)
	if name == "." || name == "/" {
		name = "vm-image"
	}

	dir := filepath.Join(basePath, VMImagesSubvolume)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create vm-images directory: %w", err)
	}

	// Download the image.
	ctx, cancel := context.WithTimeout(c.Request().Context(), 30*time.Minute)
	defer cancel()

	downloadPath := filepath.Join(dir, name+".download")
	if err := downloadFile(ctx, req.URL, downloadPath); err != nil {
		return fmt.Errorf("download vm image: %w", err)
	}

	// Convert to raw format using qemu-img.
	rawName := strings.TrimSuffix(name, filepath.Ext(name)) + ".raw"
	rawPath := filepath.Join(dir, rawName)
	if err := convertVMImage(ctx, downloadPath, rawPath); err != nil {
		// Clean up download file on conversion failure.
		if rmErr := os.Remove(downloadPath); rmErr != nil {
			slog.Debug(fmt.Sprintf("remove download file: %v", rmErr))
		}
		return fmt.Errorf("convert vm image: %w", err)
	}

	// Remove the original download file.
	if err := os.Remove(downloadPath); err != nil {
		slog.Debug(fmt.Sprintf("remove download file: %v", err))
	}

	return c.JSON(200, VMImageInfo{Name: rawName})
}

// deleteVMImage removes a VM image from the vm-images subvolume.
//
// POST /vm-images/delete
// Body: {"name": "image.raw"}.
func (s *SystemControllerHandlers) deleteVMImage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := struct {
		Name string `json:"name"`
	}{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Name == "" {
		return errors.New("name is required")
	}

	basePath := s.Controller.GetBtrfsBasePath()
	if basePath == "" {
		return errors.New("storage not configured")
	}

	// Sanitize filename.
	name := filepath.Base(req.Name)
	imgPath := filepath.Join(basePath, VMImagesSubvolume, name)

	if _, err := os.Stat(imgPath); err != nil {
		if os.IsNotExist(err) {
			return ErrVMImageNotFound
		}
		return err
	}

	if err := os.Remove(imgPath); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

// vmImageClient is the only HTTP client used to fetch a VM image.
//
// The URL is submitted by a caller and dialled by the controller, which sits on
// the host network with the podman socket — so without a guard it reaches
// loopback services, the LAN, and link-local metadata endpoints from a position
// no remote caller has. That is the same shape as an OAuth device flow, and the
// guard is the same one: packages.CheckOAuthAddr in the dialer's Control hook,
// which runs after resolution with the concrete address about to be connected.
//
// Control rather than a check on the URL, for the reason spelled out in
// oauthClient: a URL check sees a hostname, and a hostname can answer with
// 127.0.0.1. Redirects are covered because every hop dials through the same
// dialer, and CheckRedirect refuses a scheme change on top.
func vmImageClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			return packages.CheckOAuthAddr(network, address)
		},
	}
	return &http.Client{
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("%w: redirect to %q", packages.ErrOAuthURLNotAllowed, req.URL.Scheme)
			}
			return nil
		},
	}
}

// downloadFile downloads a file from a URL to a local path.
func downloadFile(ctx context.Context, url, destPath string) (err error) {
	// A scheme check before anything else: file:// would turn this into an
	// arbitrary local read, and no other scheme is a way to fetch a disk image.
	if err := validateVMImageURL(url); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	client := vmImageClient()
	resp, err := client.Do(req) //nolint:gosec // G704 -- URL scheme and every dialled address are checked above
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath) //nolint:gosec // G304 -- destPath from internal resolveVMImagePath
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	_, err = io.Copy(f, resp.Body)
	return err
}

// convertVMImage converts a VM disk image to raw format using qemu-img.
// The source file is read and a new raw file is written to destPath.
func convertVMImage(ctx context.Context, srcPath, destPath string) error {
	cmd := exec.CommandContext(ctx, "qemu-img", "convert", "-O", "raw", srcPath, destPath) //nolint:gosec // G204 -- paths from internal resolveVMImagePath
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %w", ErrVMImageConvert, err)
	}
	return nil
}

// validateVMImageURL rejects anything that is not an HTTP(S) fetch.
//
// file:// is the one that matters — it turns "download a disk image" into an
// arbitrary local read whose result is then served back as a cached VM image —
// but there is no scheme other than http and https that names a disk image
// this code knows how to fetch, so the check is an allowlist.
func validateVMImageURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %w", packages.ErrOAuthURLNotAllowed, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q is not allowed for a VM image URL", packages.ErrOAuthURLNotAllowed, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: VM image URL has no host", packages.ErrOAuthURLNotAllowed)
	}
	return nil
}

// resolveVMImagePath resolves the VM image path for a compiled package.
// If the image is a URL, it returns the cached path in the vm-images subvolume.
// If it is a plain filename, it looks it up in the vm-images subvolume.
//
// filepath.Base is applied to the derived name, matching what uploadVMImage and
// deleteVMImage already do. Without it a package manifest naming
// "../../tls/ca.key" resolves outside the subvolume — and the result is handed
// to qemu as `-drive file=` and to qemu-img as a conversion target, so it is
// both a read of whatever it names and a write over it.
func resolveVMImagePath(basePath string, vmImage string) string {
	if strings.HasPrefix(vmImage, "http://") || strings.HasPrefix(vmImage, "https://") {
		// Derive cached filename from URL.
		parts := strings.Split(vmImage, "/")
		name := parts[len(parts)-1]
		rawName := strings.TrimSuffix(name, filepath.Ext(name)) + ".raw"
		return filepath.Join(basePath, VMImagesSubvolume, vmImageBase(rawName))
	}
	// Local reference: look in vm-images subvolume.
	if !strings.HasSuffix(vmImage, ".raw") {
		vmImage = strings.TrimSuffix(vmImage, filepath.Ext(vmImage)) + ".raw"
	}
	return filepath.Join(basePath, VMImagesSubvolume, vmImageBase(vmImage))
}

// vmImageBase reduces a reference to a single filename component. A name that
// reduces to nothing usable becomes a fixed placeholder, so the result is
// always inside the subvolume rather than being the subvolume itself.
func vmImageBase(name string) string {
	base := filepath.Base(name)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return "vm-image.raw"
	}
	return base
}
