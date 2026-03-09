// Package ui manages the Town OS UI as a system service. The UI runs as a
// systemd-supervised podman container with Restart=always, serving the web
// interface via Caddy on port 80.
package ui

import (
	"context"
	"fmt"

	"gitea.com/town-os/town-os/src/systemd"
)

// SystemService describes the UI system service metadata.
type SystemService struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Image       string `json:"image"`
	Port        string `json:"port"`
	UnitName    string `json:"unit_name"`
}

// Config holds the configuration for the UI service.
type Config struct {
	// Systemd manages systemd unit lifecycle.
	Systemd systemd.Manager
	// Image is the container image reference for the UI.
	Image string
}

// Manager controls the lifecycle of the UI container.
type Manager struct {
	cfg Config
}

// NewManager creates a new UI Manager with the given configuration.
// Call Start to boot the UI service.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// Start installs, enables, and starts the UI systemd unit. It uses the
// stop-before-start pattern to ensure the unit picks up config changes.
func (m *Manager) Start(ctx context.Context) error {
	for _, unit := range m.unitConfigs() {
		uf := systemd.GenerateSystemServiceUnit(unit)
		if err := m.cfg.Systemd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
			return fmt.Errorf("install unit %s: %w", uf.Name, err)
		}
		if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable unit %s: %w", uf.Name, err)
		}
		// Stop before Start to ensure the unit picks up the new
		// configuration. Ignore stop errors — the unit may not be running.
		_ = m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Stop)
		if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Start); err != nil {
			return fmt.Errorf("start unit %s: %w", uf.Name, err)
		}
	}

	return nil
}

// Stop is a no-op — system services persist across controller restarts.
func (m *Manager) Stop() {}

// SystemServices returns metadata for the UI system service.
func (m *Manager) SystemServices() []SystemService {
	return []SystemService{
		{
			Key:         "ui",
			DisplayName: "Town OS UI",
			Image:       m.cfg.Image,
			Port:        "80",
			UnitName:    systemd.SystemServiceUnitName("ui"),
		},
	}
}

// unitConfigs returns the systemd unit configurations for the UI service.
func (m *Manager) unitConfigs() []systemd.SystemServiceUnitConfig {
	return []systemd.SystemServiceUnitConfig{
		{
			Key:         "ui",
			Description: "Town OS UI",
			Image:       m.cfg.Image,
			Args:        []string{"--net", "host"},
		},
	}
}
