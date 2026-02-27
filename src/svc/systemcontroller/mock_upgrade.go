package systemcontroller

import "context"

// --- Upgrades ---

func (m *MockClient) ListUpgrades(_ context.Context) ([]PackageUpgrade, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListUpgrades", Args: nil})

	if m.ListUpgradesErr != nil {
		return nil, m.ListUpgradesErr
	}

	out := make([]PackageUpgrade, len(m.UpgradesList))
	copy(out, m.UpgradesList)
	return out, nil
}

func (m *MockClient) DismissUpgrades(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "DismissUpgrades", Args: nil})

	if m.DismissUpgradesErr != nil {
		return m.DismissUpgradesErr
	}
	return nil
}
