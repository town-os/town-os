package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// --- Upgrades ---

// ListUpgrades returns packages that have newer versions available in their
// source repositories.
func (c *SystemdClient) ListUpgrades(ctx context.Context) (_ []PackageUpgrade, err error) {
	resp, err := c.getClient(ctx, "packages/upgrades")
	if err != nil {
		return nil, fmt.Errorf("%w: ListUpgrades: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "packages/upgrades")
	}

	var upgrades []PackageUpgrade
	return upgrades, json.NewDecoder(resp.Body).Decode(&upgrades)
}

// DismissUpgrades marks all pending upgrade notifications as dismissed.
func (c *SystemdClient) DismissUpgrades(ctx context.Context) error {
	return c.postClient(ctx, "packages/upgrades/dismiss", nil)
}
