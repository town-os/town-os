package systemd

import (
	"context"
	"errors"
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
	Name        string
	Description string
	LoadState   string
	ActiveState string
	SubState    string
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

type Manager interface {
	ListUnits(ctx context.Context) ([]UnitStatus, error)
	SetStatus(ctx context.Context, unit string, action StatusAction) error
	LogReplay(ctx context.Context, unit string) (<-chan JournalEntry, error)
}
