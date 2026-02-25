package systemd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type StatusAction string

const (
	Start   StatusAction = "start"
	Stop    StatusAction = "stop"
	Restart StatusAction = "restart"
	Enable  StatusAction = "enable"
	Disable StatusAction = "disable"
)

var (
	ErrInvalidAction = errors.New("invalid status action")
	ErrUnitNotFound  = errors.New("unit not found")
)

type UnitStatus struct {
	Name          string
	Description   string
	LoadState     string
	ActiveState   string
	SubState      string
	UnitFileState string
}

type JournalEntry struct {
	Cursor             string
	RealtimeTimestamp   time.Time
	MonotonicTimestamp  uint64

	// User fields
	Message          string
	MessageID        string
	Priority         string
	CodeFile         string
	CodeLine         string
	CodeFunc         string
	Errno            string
	SyslogFacility   string
	SyslogIdentifier string
	SyslogPID        string

	// Trusted fields
	PID                    string
	UID                    string
	GID                    string
	Comm                   string
	Exe                    string
	Cmdline                string
	CapEffective           string
	AuditSession           string
	AuditLoginUID          string
	SystemdCGroup          string
	SystemdSession         string
	SystemdUnit            string
	SystemdUserUnit        string
	SystemdOwnerUID        string
	SystemdSlice           string
	SELinuxContext          string
	SourceRealtimeTimestamp string
	BootID                 string
	MachineID              string
	Hostname               string
	Transport              string
}

type LogTailParams struct {
	Unit         string
	Lines        int
	BeforeCursor string
	AfterCursor  string
	Grep         string
	Since        time.Time
	Until        time.Time
}

type LogTailResult struct {
	Entries   []JournalEntry `json:"entries"`
	Cursor    string         `json:"cursor"`
	EndCursor string         `json:"end_cursor"`
}

type Manager interface {
	ListUnits(ctx context.Context) ([]UnitStatus, error)
	SetStatus(ctx context.Context, unit string, action StatusAction) error
	LogReplay(ctx context.Context, unit string) (<-chan JournalEntry, error)
	LogTail(ctx context.Context, params LogTailParams) (LogTailResult, error)
	InstallUnit(ctx context.Context, name string, content string) error
	UninstallUnit(ctx context.Context, name string) error
	ListPackageUnitFiles(ctx context.Context, repo, pkgName, version string) ([]string, error)
}

// PackageUnitPrefix is the prefix for all package-related systemd units.
const PackageUnitPrefix = "town-os-package--"

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
