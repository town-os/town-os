package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ListLocales returns the list of supported locales, including the current
// system locale, populated (translated) locales, common languages with
// native names, and extended country-specific locale codes.
//
// Calls GET /locales on the Control Plane Service.
func (c *SystemdClient) ListLocales(ctx context.Context) (_ *LocaleListResponse, err error) {
	resp, err := c.getClient(ctx, "locales")
	if err != nil {
		return nil, fmt.Errorf("%w: ListLocales: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "locales")
	}

	var result LocaleListResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}
