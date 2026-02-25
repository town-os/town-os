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
	Name          string `json:"Name"`
	Description   string `json:"Description"`
	LoadState     string `json:"LoadState"`
	ActiveState   string `json:"ActiveState"`
	SubState      string `json:"SubState"`
	UnitFileState string `json:"UnitFileState"`
}

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
