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

// GetSettings returns all system settings as a key-value map.
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

// GetSetting returns the value of a single system setting by key.
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

// SetSetting creates or updates a system setting.
func (c *SystemdClient) SetSetting(ctx context.Context, key, value string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, SetSettingRequest{Key: key, Value: value})

	return c.postClient(ctx, "settings/set", pr)
}
