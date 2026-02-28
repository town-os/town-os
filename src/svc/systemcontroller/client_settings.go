package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// --- Settings ---

// GetSettings returns all system settings as a key-value map. Default settings
// include "default_quota" (50 GB), "max_archive_size" (20 MB), and
// "archive_unpack_timeout" (120 seconds).
//
// Calls GET /settings on the Control Plane Service.
func (c *SystemdClient) GetSettings(ctx context.Context) (_ map[string]string, err error) {
	resp, err := c.getClient(ctx, "settings")
	if err != nil {
		return nil, fmt.Errorf("%w: GetSettings: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "settings")
	}

	var settings map[string]string
	return settings, json.NewDecoder(resp.Body).Decode(&settings)
}

// GetSetting returns the value of a single system setting by key. Valid keys
// are "default_quota", "max_archive_size", and "archive_unpack_timeout".
//
// Parameters:
//   - key: the setting key to retrieve.
//
// Calls POST /settings/get on the Control Plane Service.
func (c *SystemdClient) GetSetting(ctx context.Context, key string) (_ string, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, GetSettingRequest{Key: key})

	resp, err := c.postJSON(ctx, "settings/get", pr)
	if err != nil {
		return "", fmt.Errorf("%w: GetSetting: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return "", readProblemDetail(resp, "POST", "settings/get")
	}

	var result struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", err
	}
	return result.Value, nil
}

// SetSetting creates or updates a system setting. Byte-value settings
// ("default_quota", "max_archive_size") accept human-readable strings such as
// "500GB" or "10MB", which are parsed and stored as numeric byte counts.
//
// Parameters:
//   - key: the setting key to set.
//   - value: the new value. For byte-value settings, accepts human-readable
//     byte strings (e.g., "50GB", "20MB"). For "archive_unpack_timeout", the
//     value is stored as a number of seconds.
//
// Calls POST /settings/set on the Control Plane Service.
func (c *SystemdClient) SetSetting(ctx context.Context, key, value string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, SetSettingRequest{Key: key, Value: value})

	return c.postClient(ctx, "settings/set", pr)
}
