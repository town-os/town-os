package systemd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	// Use ListUnitsByPatternsContext with empty states to include units in
	// all states (active, inactive, not-loaded). ListUnitsContext only
	// returns loaded units, so stopped-and-unloaded services would be
	// missing from the results.
	units, err := conn.ListUnitsByPatternsContext(ctx, []string{}, []string{"*"})
	if err != nil {
		return nil, err
	}

	fileStateMap := make(map[string]string)
	files, err := conn.ListUnitFilesContext(ctx)
	if err == nil {
		for _, f := range files {
			fileStateMap[filepath.Base(f.Path)] = f.Type
		}
	}

	result := make([]UnitStatus, len(units))
	for i, u := range units {
		state := fileStateMap[u.Name]
		if state == "" {
			prop, err := conn.GetUnitPropertyContext(ctx, u.Name, "UnitFileState")
			if err == nil && prop.Value.Value() != nil {
				if s, ok := prop.Value.Value().(string); ok {
					state = s
				}
			}
		}
		result[i] = UnitStatus{
			Name:          u.Name,
			Description:   u.Description,
			LoadState:     u.LoadState,
			ActiveState:   u.ActiveState,
			SubState:      u.SubState,
			UnitFileState: state,
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

func (m *SystemdManager) InstallUnit(ctx context.Context, name string, content string) (err error) {
	unitPath := "/etc/systemd/system/" + name

	f, err := os.Create(unitPath)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	n, err := io.Copy(f, strings.NewReader(content))
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("wrote 0 bytes to %s", unitPath)
	}

	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	return conn.ReloadContext(ctx)
}

func (m *SystemdManager) ListPackageUnitFiles(_ context.Context, pkgName string) ([]string, error) {
	pattern := fmt.Sprintf("/etc/systemd/system/town-os-%s*", pkgName)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(matches))
	for i, match := range matches {
		names[i] = filepath.Base(match)
	}
	sort.Strings(names)

	return names, nil
}

func (m *SystemdManager) UninstallUnit(ctx context.Context, name string) error {
	unitPath := "/etc/systemd/system/" + name

	if err := os.Remove(unitPath); err != nil {
		return err
	}

	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	return conn.ReloadContext(ctx)
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

func (m *SystemdManager) LogTail(ctx context.Context, p LogTailParams) (_ LogTailResult, err error) {
	j, err := sdjournal.NewJournal()
	if err != nil {
		return LogTailResult{}, err
	}
	defer func() {
		err = errors.Join(err, j.Close())
	}()

	if p.Unit != "" {
		if err := j.AddMatch(fmt.Sprintf("_SYSTEMD_UNIT=%s", p.Unit)); err != nil {
			return LogTailResult{}, err
		}
	}

	grepLower := strings.ToLower(p.Grep)

	matchesGrep := func(je JournalEntry) bool {
		if p.Grep == "" {
			return true
		}
		return strings.Contains(strings.ToLower(je.Message), grepLower)
	}

	collectForward := func() (LogTailResult, error) {
		entries := make([]JournalEntry, 0, p.Lines)
		for len(entries) < p.Lines {
			n, err := j.Next()
			if err != nil {
				return LogTailResult{}, err
			}
			if n == 0 {
				break
			}
			entry, err := j.GetEntry()
			if err != nil {
				return LogTailResult{}, err
			}
			je := journalEntryFromSD(entry)
			if !p.Until.IsZero() && !je.RealtimeTimestamp.Before(p.Until) {
				break
			}
			if !matchesGrep(je) {
				continue
			}
			entries = append(entries, je)
		}

		var cursor, endCursor string
		if len(entries) > 0 {
			cursor = entries[0].Cursor
			endCursor = entries[len(entries)-1].Cursor
		}
		return LogTailResult{Entries: entries, Cursor: cursor, EndCursor: endCursor}, nil
	}

	// Timestamp seek mode: get entries from a specific time forward.
	if !p.Since.IsZero() && p.AfterCursor == "" && p.BeforeCursor == "" {
		if err := j.SeekRealtimeUsec(uint64(p.Since.UnixMicro())); err != nil {
			return LogTailResult{}, err
		}
		return collectForward()
	}

	// Forward mode: get entries after a given cursor.
	if p.AfterCursor != "" {
		if err := j.SeekCursor(p.AfterCursor); err != nil {
			return LogTailResult{}, err
		}
		// SeekCursor lands on the cursor entry; advance past it.
		if _, err := j.Next(); err != nil {
			return LogTailResult{}, err
		}
		return collectForward()
	}

	// Backward mode: get entries before a given cursor (or from tail).
	if p.BeforeCursor != "" {
		if err := j.SeekCursor(p.BeforeCursor); err != nil {
			return LogTailResult{}, err
		}
		// SeekCursor lands on the cursor entry; move back one so we don't include it.
		if _, err := j.Previous(); err != nil {
			return LogTailResult{}, err
		}
	} else {
		if err := j.SeekTail(); err != nil {
			return LogTailResult{}, err
		}
	}

	entries := make([]JournalEntry, 0, p.Lines)
	for len(entries) < p.Lines {
		n, err := j.Previous()
		if err != nil {
			return LogTailResult{}, err
		}
		if n == 0 {
			break
		}
		entry, err := j.GetEntry()
		if err != nil {
			return LogTailResult{}, err
		}
		je := journalEntryFromSD(entry)
		if !matchesGrep(je) {
			continue
		}
		entries = append(entries, je)
	}

	// entries are in reverse chronological order; flip to chronological
	for i, k := 0, len(entries)-1; i < k; i, k = i+1, k-1 {
		entries[i], entries[k] = entries[k], entries[i]
	}

	var cursor, endCursor string
	if len(entries) > 0 {
		cursor = entries[0].Cursor
		endCursor = entries[len(entries)-1].Cursor
	}

	return LogTailResult{Entries: entries, Cursor: cursor, EndCursor: endCursor}, nil
}

func (m *SystemdManager) LogReplay(ctx context.Context, unit string) (_ <-chan JournalEntry, err error) {
	j, err := sdjournal.NewJournal()
	if err != nil {
		return nil, err
	}

	if unit != "" {
		err = j.AddMatch(fmt.Sprintf("_SYSTEMD_UNIT=%s", unit))
		if err != nil {
			return nil, errors.Join(err, j.Close())
		}
	}

	err = j.SeekHead()
	if err != nil {
		return nil, errors.Join(err, j.Close())
	}

	ch := make(chan JournalEntry)

	go func() {
		defer close(ch)
		defer func() {
			err = errors.Join(err, j.Close())
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
