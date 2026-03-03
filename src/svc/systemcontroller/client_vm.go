package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ListVMImages returns all cached VM disk images in the vm-images subvolume.
// Each entry includes the image filename and size in bytes.
//
// Calls GET /vm-images on the Control Plane Service.
func (c *SystemdClient) ListVMImages(ctx context.Context) (_ []VMImageInfo, err error) {
	resp, err := c.getClient(ctx, "vm-images")
	if err != nil {
		return nil, fmt.Errorf("%w: ListVMImages: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "vm-images")
	}

	var images []VMImageInfo
	return images, json.NewDecoder(resp.Body).Decode(&images)
}

// UploadVMImage downloads a VM disk image from a remote URL, converts it to
// raw format using qemu-img, and caches the result in the vm-images subvolume.
//
// Parameters:
//   - url: remote URL to download the VM image from (required).
//   - name: desired filename for the cached image. When empty, the filename
//     is derived from the URL's last path segment.
//
// Calls POST /vm-images/upload on the Control Plane Service.
func (c *SystemdClient) UploadVMImage(ctx context.Context, url, name string) (_ *VMImageInfo, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, UploadVMImageRequest{URL: url, Name: name})

	resp, err := c.postJSON(ctx, "vm-images/upload", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: UploadVMImage: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "vm-images/upload")
	}

	var info VMImageInfo
	return &info, json.NewDecoder(resp.Body).Decode(&info)
}

// DeleteVMImage removes a cached VM disk image from the vm-images subvolume.
//
// Parameters:
//   - name: filename of the VM image to delete (required).
//
// Calls POST /vm-images/delete on the Control Plane Service.
func (c *SystemdClient) DeleteVMImage(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, struct {
		Name string `json:"name"`
	}{Name: name})

	return c.postClient(ctx, "vm-images/delete", pr)
}
