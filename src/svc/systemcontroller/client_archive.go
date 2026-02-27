package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// --- Archive ---

// UploadArchive uploads and extracts an archive into the named subvolume.
// Supported formats are tar.gz, tar.bz2, and tar.xz (detected from the
// filename extension). The archive data is read from archiveReader and sent
// as a multipart form upload. When subpath is non-empty, extraction is
// limited to that directory within the subvolume. When stopService is
// non-empty, the named systemd service is stopped before extraction and
// restarted afterward.
func (c *SystemdClient) UploadArchive(ctx context.Context, subvolume string, archiveReader io.Reader, filename, subpath, stopService string) (_ *ArchiveUploadResponse, err error) {
	pr, pw := io.Pipe()

	writer := multipart.NewWriter(pw)
	go func() {
		if err := writer.WriteField("subvolume", subvolume); err != nil {
			pw.CloseWithError(err)
			return
		}
		if subpath != "" {
			if err := writer.WriteField("subpath", subpath); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		if stopService != "" {
			if err := writer.WriteField("stop_service", stopService); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		part, err := writer.CreateFormFile("archive", filename)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, archiveReader); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(writer.Close())
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route("storage/upload-archive"), pr)
	if err != nil {
		return nil, fmt.Errorf("%w: POST storage/upload-archive: %w", ErrNewRequest, err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: POST storage/upload-archive: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "storage/upload-archive")
	}

	var result ArchiveUploadResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}

// DownloadArchive creates an archive of the specified paths within the named
// subvolume and returns a reader for the archive data. The format parameter
// selects the compression: "tar.gz", "tar.bz2", or "tar.xz". When stopService
// is non-empty, the named systemd service is stopped during archival. The
// caller must close the returned [io.ReadCloser].
func (c *SystemdClient) DownloadArchive(ctx context.Context, subvolume string, paths []string, stopService, format string) (_ io.ReadCloser, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, DownloadArchiveRequest{Subvolume: subvolume, Paths: paths, StopService: stopService, Format: format})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route("storage/download-archive"), pr)
	if err != nil {
		return nil, fmt.Errorf("%w: POST storage/download-archive: %w", ErrNewRequest, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: POST storage/download-archive: %w", ErrHTTPRequest, err)
	}

	if resp.StatusCode != http.StatusOK {
		defer func() {
			err = errors.Join(err, resp.Body.Close())
		}()
		return nil, readProblemDetail(resp, "POST", "storage/download-archive")
	}

	return resp.Body, nil
}
