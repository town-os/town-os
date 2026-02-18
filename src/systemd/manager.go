package systemd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/coreos/go-systemd/v22/sdjournal"
)

type SystemdManager struct{}

func NewManager() *SystemdManager {
	return &SystemdManager{}
}

func (m *SystemdManager) ListUnits(ctx context.Context) ([]UnitStatus, error) {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	units, err := conn.ListUnitsContext(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]UnitStatus, len(units))
	for i, u := range units {
		result[i] = UnitStatus{
			Name:        u.Name,
			Description: u.Description,
			LoadState:   u.LoadState,
			ActiveState: u.ActiveState,
			SubState:    u.SubState,
		}
	}

	return result, nil
}

func (m *SystemdManager) SetStatus(ctx context.Context, unit string, action StatusAction) error {
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	switch action {
	case Start:
		ch := make(chan string, 1)
		_, err = conn.StartUnitContext(ctx, unit, "replace", ch)
		if err != nil {
			return err
		}
		<-ch
	case Stop:
		ch := make(chan string, 1)
		_, err = conn.StopUnitContext(ctx, unit, "replace", ch)
		if err != nil {
			return err
		}
		<-ch
	case Restart:
		ch := make(chan string, 1)
		_, err = conn.RestartUnitContext(ctx, unit, "replace", ch)
		if err != nil {
			return err
		}
		<-ch
	case Enable:
		_, _, err = conn.EnableUnitFilesContext(ctx, []string{unit}, false, false)
		if err != nil {
			return err
		}
		return conn.ReloadContext(ctx)
	case Disable:
		_, err = conn.DisableUnitFilesContext(ctx, []string{unit}, false)
		if err != nil {
			return err
		}
		return conn.ReloadContext(ctx)
	default:
		return fmt.Errorf("%q: %w", action, ErrInvalidAction)
	}

	return nil
}

func journalEntryFromSD(entry *sdjournal.JournalEntry) JournalEntry {
	return JournalEntry{
		Cursor:            entry.Cursor,
		RealtimeTimestamp:  time.UnixMicro(int64(entry.RealtimeTimestamp)),
		MonotonicTimestamp: entry.MonotonicTimestamp,

		Message:          entry.Fields[sdjournal.SD_JOURNAL_FIELD_MESSAGE],
		MessageID:        entry.Fields[sdjournal.SD_JOURNAL_FIELD_MESSAGE_ID],
		Priority:         entry.Fields[sdjournal.SD_JOURNAL_FIELD_PRIORITY],
		CodeFile:         entry.Fields[sdjournal.SD_JOURNAL_FIELD_CODE_FILE],
		CodeLine:         entry.Fields[sdjournal.SD_JOURNAL_FIELD_CODE_LINE],
		CodeFunc:         entry.Fields[sdjournal.SD_JOURNAL_FIELD_CODE_FUNC],
		Errno:            entry.Fields[sdjournal.SD_JOURNAL_FIELD_ERRNO],
		SyslogFacility:   entry.Fields[sdjournal.SD_JOURNAL_FIELD_SYSLOG_FACILITY],
		SyslogIdentifier: entry.Fields[sdjournal.SD_JOURNAL_FIELD_SYSLOG_IDENTIFIER],
		SyslogPID:        entry.Fields[sdjournal.SD_JOURNAL_FIELD_SYSLOG_PID],

		PID:                    entry.Fields[sdjournal.SD_JOURNAL_FIELD_PID],
		UID:                    entry.Fields[sdjournal.SD_JOURNAL_FIELD_UID],
		GID:                    entry.Fields[sdjournal.SD_JOURNAL_FIELD_GID],
		Comm:                   entry.Fields[sdjournal.SD_JOURNAL_FIELD_COMM],
		Exe:                    entry.Fields[sdjournal.SD_JOURNAL_FIELD_EXE],
		Cmdline:                entry.Fields[sdjournal.SD_JOURNAL_FIELD_CMDLINE],
		CapEffective:           entry.Fields[sdjournal.SD_JOURNAL_FIELD_CAP_EFFECTIVE],
		AuditSession:           entry.Fields[sdjournal.SD_JOURNAL_FIELD_AUDIT_SESSION],
		AuditLoginUID:          entry.Fields[sdjournal.SD_JOURNAL_FIELD_AUDIT_LOGINUID],
		SystemdCGroup:          entry.Fields[sdjournal.SD_JOURNAL_FIELD_SYSTEMD_CGROUP],
		SystemdSession:         entry.Fields[sdjournal.SD_JOURNAL_FIELD_SYSTEMD_SESSION],
		SystemdUnit:            entry.Fields[sdjournal.SD_JOURNAL_FIELD_SYSTEMD_UNIT],
		SystemdUserUnit:        entry.Fields[sdjournal.SD_JOURNAL_FIELD_SYSTEMD_USER_UNIT],
		SystemdOwnerUID:        entry.Fields[sdjournal.SD_JOURNAL_FIELD_SYSTEMD_OWNER_UID],
		SystemdSlice:           entry.Fields[sdjournal.SD_JOURNAL_FIELD_SYSTEMD_SLICE],
		SELinuxContext:         entry.Fields[sdjournal.SD_JOURNAL_FIELD_SELINUX_CONTEXT],
		SourceRealtimeTimestamp: entry.Fields[sdjournal.SD_JOURNAL_FIELD_SOURCE_REALTIME_TIMESTAMP],
		BootID:                 entry.Fields[sdjournal.SD_JOURNAL_FIELD_BOOT_ID],
		MachineID:              entry.Fields[sdjournal.SD_JOURNAL_FIELD_MACHINE_ID],
		Hostname:               entry.Fields[sdjournal.SD_JOURNAL_FIELD_HOSTNAME],
		Transport:              entry.Fields[sdjournal.SD_JOURNAL_FIELD_TRANSPORT],
	}
}

func (m *SystemdManager) LogReplay(ctx context.Context, unit string) (_ <-chan JournalEntry, err error) {
	j, err := sdjournal.NewJournal()
	if err != nil {
		return nil, err
	}

	err = j.AddMatch("_SYSTEMD_UNIT=" + unit)
	if err != nil {
		return nil, errors.Join(err, j.Close())
	}

	err = j.SeekHead()
	if err != nil {
		return nil, errors.Join(err, j.Close())
	}

	ch := make(chan JournalEntry)

	go func() {
		defer close(ch)
		defer func() {
			if cerr := j.Close(); cerr != nil && err == nil {
				err = cerr
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, err := j.Next()
			if err != nil {
				return
			}

			if n == 0 {
				select {
				case <-ctx.Done():
					return
				default:
					j.Wait(time.Second)
					continue
				}
			}

			entry, err := j.GetEntry()
			if err != nil {
				return
			}

			select {
			case ch <- journalEntryFromSD(entry):
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
