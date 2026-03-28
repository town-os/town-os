// Package systemd provides types and interfaces for managing systemd units,
// journal log entries, and package-related service naming conventions.
package systemd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// StatusAction represents an action that can be applied to a systemd unit.
// Valid values are [Start], [Stop], [Restart], [Enable], and [Disable].
type StatusAction string

const (
	// Start starts a stopped unit.
	Start StatusAction = "start"
	// Stop stops a running unit.
	Stop StatusAction = "stop"
	// Restart stops and then starts a unit.
	Restart StatusAction = "restart"
	// Enable marks a unit to start automatically at boot.
	Enable StatusAction = "enable"
	// Disable prevents a unit from starting automatically at boot.
	Disable StatusAction = "disable"
)

// Sentinel errors returned by [Manager] implementations.
var (
	ErrInvalidAction = errors.New("invalid status action")
	ErrUnitNotFound  = errors.New("unit not found")
)

// UnitStatus represents the current state of a systemd unit. LoadState
// indicates whether the unit file was loaded ("loaded", "not-found", etc.),
// ActiveState indicates the high-level state ("active", "inactive", "failed"),
// and SubState provides the low-level state ("running", "dead", "exited", etc.).
type UnitStatus struct {
	Name          string `json:"Name"`
	Description   string `json:"Description"`
	LoadState     string `json:"LoadState"`
	ActiveState   string `json:"ActiveState"`
	SubState      string `json:"SubState"`
	UnitFileState string `json:"UnitFileState"`
}

// JournalEntry represents a single systemd journal log entry with standard
// journal fields. Cursor is opaque and can be used for pagination.
type JournalEntry struct {
	Cursor             string    `json:"Cursor"`
	RealtimeTimestamp  time.Time `json:"RealtimeTimestamp"`
	MonotonicTimestamp uint64    `json:"MonotonicTimestamp"`

	// User fields
	Message          string `json:"Message"`
	MessageID        string `json:"MessageID"`
	Priority         string `json:"Priority"`
	CodeFile         string `json:"CodeFile"`
	CodeLine         string `json:"CodeLine"`
	CodeFunc         string `json:"CodeFunc"`
	Errno            string `json:"Errno"`
	SyslogFacility   string `json:"SyslogFacility"`
	SyslogIdentifier string `json:"SyslogIdentifier"`
	SyslogPID        string `json:"SyslogPID"`

	// Trusted fields
	PID                    string `json:"PID"`
	UID                    string `json:"UID"`
	GID                    string `json:"GID"`
	Comm                   string `json:"Comm"`
	Exe                    string `json:"Exe"`
	Cmdline                string `json:"Cmdline"`
	CapEffective           string `json:"CapEffective"`
	AuditSession           string `json:"AuditSession"`
	AuditLoginUID          string `json:"AuditLoginUID"`
	SystemdCGroup          string `json:"SystemdCGroup"`
	SystemdSession         string `json:"SystemdSession"`
	SystemdUnit            string `json:"SystemdUnit"`
	SystemdUserUnit        string `json:"SystemdUserUnit"`
	SystemdOwnerUID        string `json:"SystemdOwnerUID"`
	SystemdSlice           string `json:"SystemdSlice"`
	SELinuxContext          string `json:"SELinuxContext"`
	SourceRealtimeTimestamp string `json:"SourceRealtimeTimestamp"`
	BootID                 string `json:"BootID"`
	MachineID              string `json:"MachineID"`
	Hostname               string `json:"Hostname"`
	Transport              string `json:"Transport"`
}

// LogTailParams configures a paginated journal log query.
//
//   - Unit: the systemd unit name to query (required).
//   - Lines: maximum number of entries to return.
//   - BeforeCursor: return entries before this opaque cursor (for backward paging).
//   - AfterCursor: return entries after this opaque cursor (for forward paging).
//   - Grep: filter entries whose message matches this substring.
//   - Since: only include entries at or after this time (zero means no lower bound).
//   - Until: only include entries at or before this time (zero means no upper bound).
//   - Priority: maximum syslog priority level to include; 0 means no filter,
//     values 1–7 include entries with priority <= the specified value
//     (e.g. 3 returns emergency, alert, critical, and error).
type LogTailParams struct {
	Unit         string
	Lines        int
	BeforeCursor string
	AfterCursor  string
	Grep         string
	Since        time.Time
	Until        time.Time
	Priority     int // 0 = no filter; 1–7 = include entries with priority <= this value
}

// LogTailResult is the response from a log tail query. Cursor is the opaque
// cursor of the first entry; EndCursor is the cursor of the last entry.
// Use these cursors with [LogTailParams].BeforeCursor and AfterCursor for
// pagination.
type LogTailResult struct {
	Entries   []JournalEntry `json:"entries"`
	Cursor    string         `json:"cursor"`
	EndCursor string         `json:"end_cursor"`
}

// Manager defines the interface for systemd unit management. Implementations
// handle unit lifecycle, journal log access, and package unit file installation.
type Manager interface {
	// ListUnits returns all managed systemd units and their current status.
	ListUnits(ctx context.Context) ([]UnitStatus, error)
	// SetStatus applies a [StatusAction] to the named unit. Valid actions are
	// [Start], [Stop], [Restart], [Enable], and [Disable].
	SetStatus(ctx context.Context, unit string, action StatusAction) error
	// LogReplay streams historical journal entries for the named unit. The
	// returned channel is closed when all matching entries have been sent.
	LogReplay(ctx context.Context, unit string) (<-chan JournalEntry, error)
	// LogTail returns a page of journal entries for the named unit with
	// cursor-based pagination. See [LogTailParams] for filtering options.
	LogTail(ctx context.Context, params LogTailParams) (LogTailResult, error)
	// InstallUnit writes a systemd unit file with the given name and content,
	// reloads the daemon, and enables the unit.
	InstallUnit(ctx context.Context, name string, content string) error
	// UninstallUnit stops, disables, and removes a systemd unit file.
	UninstallUnit(ctx context.Context, name string) error
	// ListPackageUnitFiles returns the systemd unit file names associated with
	// the package identified by repo, pkgName, and version.
	ListPackageUnitFiles(ctx context.Context, repo, pkgName, version string) ([]string, error)
	// ReadUnit reads the content of a systemd unit file by name. Returns
	// ErrUnitNotFound if the file does not exist.
	ReadUnit(name string) (string, error)
}

// PackageUnitPrefix is the prefix for all package-related systemd units.
const PackageUnitPrefix = "town-os-package--"

// SystemServiceUnitPrefix is the prefix for all system service systemd units.
const SystemServiceUnitPrefix = "town-os-system--"

// SystemServiceUnitName returns the systemd service unit name for a system service key.
func SystemServiceUnitName(key string) string {
	return fmt.Sprintf("%s%s.service", SystemServiceUnitPrefix, key)
}

// SystemServiceContainerName returns the podman container name for a system service key.
func SystemServiceContainerName(key string) string {
	return fmt.Sprintf("%s%s", SystemServiceUnitPrefix, key)
}

// IsSystemServiceUnit returns true if the unit name is a system service unit.
func IsSystemServiceUnit(name string) bool {
	return strings.HasPrefix(name, SystemServiceUnitPrefix) && strings.HasSuffix(name, ".service")
}

// UnitName returns the systemd service unit name for a given package.
func UnitName(repo, pkgName, version string) string {
	return fmt.Sprintf("%s%s-%s-%s.service", PackageUnitPrefix, repo, pkgName, version)
}

// SocketUnitName returns the systemd socket unit name for a given package and port.
func SocketUnitName(repo, pkgName, version string, port uint16) string {
	return fmt.Sprintf("%s%s-%s-%s-%d-tcp.socket", PackageUnitPrefix, repo, pkgName, version, port)
}

// NetworkControllerUnitName returns the systemd service unit name for the
// network controller associated with the given package.
func NetworkControllerUnitName(repo, pkgName, version string) string {
	return fmt.Sprintf("%s%s-%s-%s-network.service", PackageUnitPrefix, repo, pkgName, version)
}

// ContainerName returns the podman container name for a package.
func ContainerName(repo, pkgName, version string) string {
	return fmt.Sprintf("%s%s-%s-%s", PackageUnitPrefix, repo, pkgName, version)
}

// NetworkName returns the podman network name for a package's private network.
func NetworkName(repo, pkgName, version string) string {
	return fmt.Sprintf("town-os-net--%s-%s-%s", repo, pkgName, version)
}

// NetworkControllerContainerName returns the podman container name for a
// package's network controller.
func NetworkControllerContainerName(repo, pkgName, version string) string {
	return fmt.Sprintf("%s%s-%s-%s-network", PackageUnitPrefix, repo, pkgName, version)
}

// NetworkControllerContainerNameFromUnit derives the podman container name
// from a network controller unit name by stripping the .service suffix.
func NetworkControllerContainerNameFromUnit(unitName string) string {
	return strings.TrimSuffix(unitName, ".service")
}

// IsPackageServiceUnit returns true if the unit name is a main package
// service unit (not a socket, timer, uPnP, or forwarder unit).
func IsPackageServiceUnit(name string) bool {
	if !strings.HasPrefix(name, PackageUnitPrefix) {
		return false
	}
	if !strings.HasSuffix(name, ".service") {
		return false
	}
	if strings.HasSuffix(name, "-upnp.service") {
		return false
	}
	if strings.HasSuffix(name, "-network.service") {
		return false
	}
	if strings.Contains(name, "-fwd-") {
		return false
	}
	return true
}

// StubUnitContent returns a simple Type=simple unit file that loops printing
// a running message. Useful for stub/test services.
func StubUnitContent(repo, pkgName, version string) string {
	return fmt.Sprintf(`[Unit]
Description=Town OS Package Service: %s/%s@%s

[Service]
Type=simple
ExecStart=/bin/sh -c 'while true; do echo "%s/%s@%s running"; sleep 1; done'

[Install]
WantedBy=multi-user.target
`, repo, pkgName, version, repo, pkgName, version)
}
