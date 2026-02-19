package systemd

import (
	"context"
	"errors"
	"fmt"
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

type LogTailResult struct {
	Entries []JournalEntry `json:"entries"`
	Cursor  string         `json:"cursor"`
}

type Manager interface {
	ListUnits(ctx context.Context) ([]UnitStatus, error)
	SetStatus(ctx context.Context, unit string, action StatusAction) error
	LogReplay(ctx context.Context, unit string) (<-chan JournalEntry, error)
	LogTail(ctx context.Context, unit string, lines int, beforeCursor string, grep string) (LogTailResult, error)
	InstallUnit(ctx context.Context, name string, content string) error
	UninstallUnit(ctx context.Context, name string) error
}

// UnitName returns the systemd service unit name for a given package.
func UnitName(pkgName string) string {
	return fmt.Sprintf("town-os-%s.service", pkgName)
}

// StubUnitContent returns a simple Type=simple unit file that loops printing
// a running message. Useful for stub/test services.
func StubUnitContent(pkgName, version string) string {
	return fmt.Sprintf(`[Unit]
Description=town-os %s@%s

[Service]
Type=simple
ExecStart=/bin/sh -c 'while true; do echo "%s@%s running"; sleep 1; done'

[Install]
WantedBy=multi-user.target
`, pkgName, version, pkgName, version)
}
