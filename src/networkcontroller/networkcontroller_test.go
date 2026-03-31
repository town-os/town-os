// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package networkcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/upnp"
)

// mockProcess implements Process for testing.
type mockProcess struct {
	mu      sync.Mutex
	pid     int
	killed  bool
	waitCh  chan struct{}
}

func newMockProcess(pid int) *mockProcess {
	return &mockProcess{pid: pid, waitCh: make(chan struct{})}
}

func (p *mockProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killed = true
	select {
	case <-p.waitCh:
	default:
		close(p.waitCh)
	}
	return nil
}

func (p *mockProcess) Wait() error {
	<-p.waitCh
	return nil
}

func (p *mockProcess) Pid() int { return p.pid }

func (p *mockProcess) IsKilled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}

// mockRunner implements ExecRunner for testing.
type mockRunner struct {
	mu      sync.Mutex
	calls   []mockRunnerCall
	nextPid int
	procs   []*mockProcess
}

type mockRunnerCall struct {
	Name string
	Args []string
}

func newMockRunner() *mockRunner {
	return &mockRunner{nextPid: 1000}
}

func (r *mockRunner) Start(name string, args ...string) (Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, mockRunnerCall{Name: name, Args: args})
	proc := newMockProcess(r.nextPid)
	r.procs = append(r.procs, proc)
	r.nextPid++
	return proc, nil
}

func (r *mockRunner) GetCalls() []mockRunnerCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]mockRunnerCall, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *mockRunner) GetProcs() []*mockProcess {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*mockProcess, len(r.procs))
	copy(out, r.procs)
	return out
}

func TestStateFileParsing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	state := PackageNetworkState{
		Repo:    "core",
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	err = os.WriteFile(path, data, 0600)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	parsed, err := readState(path)
	if err != nil {
		t.Fatalf("readState: %v", err)
	}

	if parsed.Repo != "core" {
		t.Fatalf("expected repo core, got %s", parsed.Repo)
	}
	if parsed.Package != "nginx" {
		t.Fatalf("expected package nginx, got %s", parsed.Package)
	}
	if parsed.Version != "1.0" {
		t.Fatalf("expected version 1.0, got %s", parsed.Version)
	}
	if len(parsed.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(parsed.Ports))
	}
	if parsed.Ports[0].ExternalPort != 8080 {
		t.Fatalf("expected external_port 8080, got %d", parsed.Ports[0].ExternalPort)
	}
}

func TestReconcileAddNewPorts(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	state := &PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	}

	ctrl.reconcile(state)

	// Verify socat was started.
	calls := runner.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(calls))
	}
	if calls[0].Name != "/usr/bin/socat" {
		t.Fatalf("expected socat, got %s", calls[0].Name)
	}
	if calls[0].Args[0] != "TCP-LISTEN:8080,fork,reuseaddr" {
		t.Fatalf("expected TCP-LISTEN:8080,fork,reuseaddr, got %s", calls[0].Args[0])
	}
	if calls[0].Args[1] != "TCP:town-os-package--test-nginx-1.0:80" {
		t.Fatalf("expected TCP:town-os-package--test-nginx-1.0:80, got %s", calls[0].Args[1])
	}

	// Verify UPnP mapping was added.
	upnpCalls := mock.GetCalls()
	if len(upnpCalls) != 1 {
		t.Fatalf("expected 1 UPnP call, got %d", len(upnpCalls))
	}
	if upnpCalls[0].Method != "AddPortMapping" {
		t.Fatalf("expected AddPortMapping, got %s", upnpCalls[0].Method)
	}

	fwds := ctrl.GetForwarders()
	if len(fwds) != 1 {
		t.Fatalf("expected 1 forwarder, got %d", len(fwds))
	}
}

func TestReconcileRemoveOldPorts(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	// Add initial state.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	})

	// Reconcile with empty state.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports:   []PortConfig{},
	})

	// Verify socat was killed.
	procs := runner.GetProcs()
	if len(procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(procs))
	}
	if !procs[0].IsKilled() {
		t.Fatal("expected socat process to be killed")
	}

	// Verify UPnP mapping was removed.
	upnpCalls := mock.GetCalls()
	found := false
	for _, c := range upnpCalls {
		if c.Method == "RemovePortMapping" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected RemovePortMapping call")
	}

	fwds := ctrl.GetForwarders()
	if len(fwds) != 0 {
		t.Fatalf("expected 0 forwarders, got %d", len(fwds))
	}
}

func TestReconcileUnchangedPortsLeftAlone(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	state := &PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	}

	ctrl.reconcile(state)

	initialExecCalls := len(runner.GetCalls())
	initialUpnpCalls := len(mock.GetCalls())

	// Reconcile with same state.
	ctrl.reconcile(state)

	// No new exec or UPnP calls.
	if len(runner.GetCalls()) != initialExecCalls {
		t.Fatalf("expected no new exec calls, got %d total", len(runner.GetCalls()))
	}
	if len(mock.GetCalls()) != initialUpnpCalls {
		t.Fatalf("expected no new UPnP calls, got %d total", len(mock.GetCalls()))
	}
}

func TestUPnPDescriptionFormat(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: false},
		},
	})

	upnpCalls := mock.GetCalls()
	if len(upnpCalls) != 1 {
		t.Fatalf("expected 1 UPnP call, got %d", len(upnpCalls))
	}

	desc, ok := upnpCalls[0].Args[3].(string)
	if !ok {
		t.Fatalf("expected string for description, got %T", upnpCalls[0].Args[3])
	}
	expected := "Town OS: Forward for nginx@1.0 on 8080"
	if desc != expected {
		t.Fatalf("expected description %q, got %q", expected, desc)
	}
}

func TestUPnPPortLogicForwardTrue(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	})

	upnpCalls := mock.GetCalls()
	if len(upnpCalls) != 1 {
		t.Fatalf("expected 1 UPnP call, got %d", len(upnpCalls))
	}

	// forward=true → AddPortMapping(ext, ext)
	extPort, ok := upnpCalls[0].Args[1].(uint16)
	if !ok {
		t.Fatalf("expected uint16 for external port, got %T", upnpCalls[0].Args[1])
	}
	intPort, ok := upnpCalls[0].Args[2].(uint16)
	if !ok {
		t.Fatalf("expected uint16 for internal port, got %T", upnpCalls[0].Args[2])
	}
	if extPort != 8080 || intPort != 8080 {
		t.Fatalf("expected AddPortMapping(8080, 8080), got (%d, %d)", extPort, intPort)
	}
}

func TestUPnPPortLogicForwardFalse(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: false},
		},
	})

	upnpCalls := mock.GetCalls()
	if len(upnpCalls) != 1 {
		t.Fatalf("expected 1 UPnP call, got %d", len(upnpCalls))
	}

	// forward=false → AddPortMapping(ext, int)
	extPort, ok := upnpCalls[0].Args[1].(uint16)
	if !ok {
		t.Fatalf("expected uint16 for external port, got %T", upnpCalls[0].Args[1])
	}
	intPort, ok := upnpCalls[0].Args[2].(uint16)
	if !ok {
		t.Fatalf("expected uint16 for internal port, got %T", upnpCalls[0].Args[2])
	}
	if extPort != 8080 || intPort != 80 {
		t.Fatalf("expected AddPortMapping(8080, 80), got (%d, %d)", extPort, intPort)
	}
}

func TestPeriodicRenewal(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: false},
		},
	})

	initialCalls := len(mock.GetCalls())

	// Trigger renewal.
	ctrl.renewUPnP()

	upnpCalls := mock.GetCalls()
	if len(upnpCalls) != initialCalls+1 {
		t.Fatalf("expected %d UPnP calls after renewal, got %d", initialCalls+1, len(upnpCalls))
	}

	lastCall := upnpCalls[len(upnpCalls)-1]
	if lastCall.Method != "AddPortMapping" {
		t.Fatalf("expected AddPortMapping on renewal, got %s", lastCall.Method)
	}
}

func TestShutdownRemovesAll(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
			{ExternalPort: 8443, InternalPort: 443, UPnP: true, Forward: true},
		},
	})

	ctrl.Shutdown()

	// All socat processes should be killed.
	for _, proc := range runner.GetProcs() {
		if !proc.IsKilled() {
			t.Fatal("expected all socat processes to be killed on shutdown")
		}
	}

	// All UPnP mappings should be removed.
	removeCount := 0
	for _, c := range mock.GetCalls() {
		if c.Method == "RemovePortMapping" {
			removeCount++
		}
	}
	if removeCount != 2 {
		t.Fatalf("expected 2 RemovePortMapping calls on shutdown, got %d", removeCount)
	}

	fwds := ctrl.GetForwarders()
	if len(fwds) != 0 {
		t.Fatalf("expected 0 forwarders after shutdown, got %d", len(fwds))
	}
	mappings := ctrl.GetMappings()
	if len(mappings) != 0 {
		t.Fatalf("expected 0 mappings after shutdown, got %d", len(mappings))
	}
}

func waitForCalls(t *testing.T, runner *mockRunner, expected int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if len(runner.GetCalls()) >= expected {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d exec calls, got %d", expected, len(runner.GetCalls()))
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestFileChangeTrigersReconcile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	// Write initial state.
	state := PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: false, Forward: true},
		},
	}
	writeState(t, path, state)

	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Run(ctx, path)
	}()

	// Wait for initial reconcile.
	waitForCalls(t, runner, 1)

	if len(runner.GetCalls()) != 1 {
		t.Fatalf("expected 1 exec call after initial reconcile, got %d", len(runner.GetCalls()))
	}

	// Modify state file.
	state.Ports = append(state.Ports, PortConfig{ExternalPort: 8443, InternalPort: 443, UPnP: false, Forward: true})
	writeState(t, path, state)

	// Wait for fsnotify to trigger reconcile.
	waitForCalls(t, runner, 2)

	if len(runner.GetCalls()) != 2 {
		t.Fatalf("expected 2 exec calls after file change, got %d", len(runner.GetCalls()))
	}

	cancel()
	<-errCh
}

func TestUPnPUnavailableDoesNotCrash(t *testing.T) {
	runner := newMockRunner()
	// nil UPnP manager = UPnP unavailable.
	ctrl := NewControllerWithRunnerAndTarget(nil, runner, "town-os-package--test-nginx-1.0")

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	})

	// Should have started socat but no UPnP calls.
	if len(runner.GetCalls()) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(runner.GetCalls()))
	}

	// renewUPnP with nil manager should not panic.
	ctrl.renewUPnP()

	// Shutdown with nil manager should not panic.
	ctrl.Shutdown()
}

func TestNoForwarderWhenForwardFalse(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: false},
		},
	})

	// No socat should be started.
	if len(runner.GetCalls()) != 0 {
		t.Fatalf("expected 0 exec calls for forward=false, got %d", len(runner.GetCalls()))
	}

	// UPnP should still be added.
	if len(mock.GetCalls()) != 1 {
		t.Fatalf("expected 1 UPnP call, got %d", len(mock.GetCalls()))
	}
}

func TestUPnPErrorDoesNotCrash(t *testing.T) {
	mock := &upnp.MockManager{AddErr: errors.New("simulated UPnP error")}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	// Should not panic despite UPnP error.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: false},
		},
	})

	// Mapping should still be tracked for renewal attempts.
	mappings := ctrl.GetMappings()
	if len(mappings) != 1 {
		t.Fatalf("expected 1 mapping tracked despite error, got %d", len(mappings))
	}
}

func TestReconcileInternalPortForward(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	// Internal port forward: Forward=true, UPnP=false.
	state := &PackageNetworkState{
		Package: "gitea",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 9999, InternalPort: 3000, UPnP: false, Forward: true},
		},
	}

	ctrl.reconcile(state)

	// Verify socat was started.
	calls := runner.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(calls))
	}
	if calls[0].Args[0] != "TCP-LISTEN:9999,fork,reuseaddr" {
		t.Fatalf("expected TCP-LISTEN:9999,fork,reuseaddr, got %s", calls[0].Args[0])
	}
	if calls[0].Args[1] != "TCP:town-os-package--test-nginx-1.0:3000" {
		t.Fatalf("expected TCP:town-os-package--test-nginx-1.0:3000, got %s", calls[0].Args[1])
	}

	// Verify UPnP mapping was NOT added.
	upnpCalls := mock.GetCalls()
	if len(upnpCalls) != 0 {
		t.Fatalf("expected 0 UPnP calls for internal port forward, got %d", len(upnpCalls))
	}

	fwds := ctrl.GetForwarders()
	if len(fwds) != 1 {
		t.Fatalf("expected 1 forwarder, got %d", len(fwds))
	}
	if fwds[9999] != 3000 {
		t.Fatalf("expected forwarder 9999->3000, got 9999->%d", fwds[9999])
	}
}

func TestReconcileInternalPortChange(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	// Initial state: ext=8080 → int=80.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	})

	if len(runner.GetCalls()) != 1 {
		t.Fatalf("expected 1 exec call after initial reconcile, got %d", len(runner.GetCalls()))
	}

	// Change internal port: ext=8080 → int=8080.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 8080, UPnP: true, Forward: true},
		},
	})

	// Old socat must be killed.
	procs := runner.GetProcs()
	if !procs[0].IsKilled() {
		t.Fatal("expected old socat process to be killed when internal port changes")
	}

	// New socat must be started with the new internal port.
	calls := runner.GetCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 exec calls, got %d", len(calls))
	}
	if calls[1].Args[1] != "TCP:town-os-package--test-nginx-1.0:8080" {
		t.Fatalf("expected new socat to target port 8080, got %s", calls[1].Args[1])
	}

	// Forwarder map should reflect the new internal port.
	fwds := ctrl.GetForwarders()
	if fwds[8080] != 8080 {
		t.Fatalf("expected forwarder 8080->8080, got 8080->%d", fwds[8080])
	}
}

func TestReconcileForwardTrueToFalse(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	// Initial state: Forward=true.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	})

	if len(runner.GetCalls()) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(runner.GetCalls()))
	}

	// Change Forward to false — socat must be stopped.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: false},
		},
	})

	procs := runner.GetProcs()
	if !procs[0].IsKilled() {
		t.Fatal("expected socat to be killed when Forward changes to false")
	}

	fwds := ctrl.GetForwarders()
	if len(fwds) != 0 {
		t.Fatalf("expected 0 forwarders after Forward=false, got %d", len(fwds))
	}

	// No new socat should have been started.
	if len(runner.GetCalls()) != 1 {
		t.Fatalf("expected no new exec calls, got %d total", len(runner.GetCalls()))
	}
}

func TestReconcileUPnPTrueToFalse(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	// Initial state: UPnP=true.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: false},
		},
	})

	mappings := ctrl.GetMappings()
	if len(mappings) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(mappings))
	}

	// Change UPnP to false — mapping must be removed.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: false, Forward: false},
		},
	})

	mappings = ctrl.GetMappings()
	if len(mappings) != 0 {
		t.Fatalf("expected 0 mappings after UPnP=false, got %d", len(mappings))
	}

	// Verify RemovePortMapping was called.
	found := false
	for _, c := range mock.GetCalls() {
		if c.Method == "RemovePortMapping" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected RemovePortMapping call when UPnP changes to false")
	}
}

func TestReconcileForwardFlagChangeUpdatesUPnPMapping(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	// Initial state: Forward=true, UPnP=true → UPnP maps ext→ext (8080→8080).
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	})

	upnpCalls := mock.GetCalls()
	if len(upnpCalls) != 1 {
		t.Fatalf("expected 1 UPnP call, got %d", len(upnpCalls))
	}
	// forward=true → UPnP internal port = external port.
	intPort, ok := upnpCalls[0].Args[2].(uint16)
	if !ok {
		t.Fatalf("expected uint16, got %T", upnpCalls[0].Args[2])
	}
	if intPort != 8080 {
		t.Fatalf("expected UPnP internal port 8080, got %d", intPort)
	}

	// Change Forward to false — UPnP mapping must be updated to ext→int (8080→80).
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: false},
		},
	})

	// Should see: RemovePortMapping + AddPortMapping.
	upnpCalls = mock.GetCalls()
	removeCount := 0
	addCount := 0
	var lastAddIntPort uint16
	for _, c := range upnpCalls {
		switch c.Method {
		case "RemovePortMapping":
			removeCount++
		case "AddPortMapping":
			addCount++
			if p, ok := c.Args[2].(uint16); ok {
				lastAddIntPort = p
			}
		}
	}

	if removeCount != 1 {
		t.Fatalf("expected 1 RemovePortMapping, got %d", removeCount)
	}
	if addCount != 2 {
		t.Fatalf("expected 2 AddPortMapping (initial + re-add), got %d", addCount)
	}
	// forward=false → UPnP internal port = actual internal port.
	if lastAddIntPort != 80 {
		t.Fatalf("expected re-added UPnP mapping with internal port 80, got %d", lastAddIntPort)
	}
}

func TestReconcileInternalPortChangeUpdatesUPnPMapping(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	// Initial: ext=8080, int=80, UPnP=true, Forward=false → maps 8080→80.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: false},
		},
	})

	// Change internal port to 8080 → should remap to 8080→8080.
	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 8080, UPnP: true, Forward: false},
		},
	})

	upnpCalls := mock.GetCalls()
	// Last AddPortMapping should use internal port 8080.
	var lastAddIntPort uint16
	for _, c := range upnpCalls {
		if c.Method == "AddPortMapping" {
			if p, ok := c.Args[2].(uint16); ok {
				lastAddIntPort = p
			}
		}
	}
	if lastAddIntPort != 8080 {
		t.Fatalf("expected re-added UPnP mapping with internal port 8080, got %d", lastAddIntPort)
	}
}

func TestUPnPTTLAndRefreshInterval(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	ctrl.reconcile(&PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: false},
		},
	})

	upnpCalls := mock.GetCalls()
	if len(upnpCalls) != 1 {
		t.Fatalf("expected 1 UPnP call, got %d", len(upnpCalls))
	}

	// Verify TTL is 1800 (30 minutes).
	ttl, ok := upnpCalls[0].Args[4].(uint32)
	if !ok {
		t.Fatalf("expected uint32 for TTL, got %T", upnpCalls[0].Args[4])
	}
	if ttl != 1800 {
		t.Fatalf("expected TTL 1800, got %d", ttl)
	}

	// Trigger renewal and verify TTL is also 1800.
	ctrl.renewUPnP()

	upnpCalls = mock.GetCalls()
	lastCall := upnpCalls[len(upnpCalls)-1]
	renewTTL, ok := lastCall.Args[4].(uint32)
	if !ok {
		t.Fatalf("expected uint32 for renewal TTL, got %T", lastCall.Args[4])
	}
	if renewTTL != 1800 {
		t.Fatalf("expected renewal TTL 1800, got %d", renewTTL)
	}
}

func TestRunEmitsStartupLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	state := PackageNetworkState{
		Repo:    "core",
		Package: "nginx",
		Version: "1.0",
		Ports:   []PortConfig{},
	}
	writeState(t, path, state)

	// Capture slog output.
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--test-nginx-1.0")

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Run(ctx, path)
	}()

	// Give Run a moment to start and log.
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-errCh

	logged := buf.String()
	if !strings.Contains(logged, "networkcontroller starting: core/nginx@1.0") {
		t.Fatalf("expected startup log message, got: %s", logged)
	}
}

func TestCustomTargetContainer(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunnerAndTarget(mock, runner, "town-os-package--core-redis-7.0")

	state := &PackageNetworkState{
		Package: "redis",
		Version: "7.0",
		Ports: []PortConfig{
			{ExternalPort: 6379, InternalPort: 6379, UPnP: true, Forward: true},
		},
	}

	ctrl.reconcile(state)

	calls := runner.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(calls))
	}
	if calls[0].Args[1] != "TCP:town-os-package--core-redis-7.0:6379" {
		t.Fatalf("expected TCP:town-os-package--core-redis-7.0:6379, got %s", calls[0].Args[1])
	}
}

func TestNoTargetContainerSkipsSocat(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	// No target container set — socat should be skipped.
	ctrl := NewControllerWithRunner(mock, runner)

	state := &PackageNetworkState{
		Package: "nginx",
		Version: "1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	}

	ctrl.reconcile(state)

	// No socat should be started (no target container).
	if len(runner.GetCalls()) != 0 {
		t.Fatalf("expected 0 exec calls without target container, got %d", len(runner.GetCalls()))
	}

	// UPnP should still be added even without a target container.
	upnpCalls := mock.GetCalls()
	if len(upnpCalls) != 1 {
		t.Fatalf("expected 1 UPnP call, got %d", len(upnpCalls))
	}
}

func TestTargetContainerFromStateFile(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	writeState(t, statePath, PackageNetworkState{
		Package:       "nginx",
		Version:       "1.0",
		ContainerName: "town-os-package--core-nginx-1.0",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: false, Forward: true},
		},
	})

	runner := newMockRunner()
	// No target set via constructor — should pick it up from the state file.
	ctrl := NewControllerWithRunner(nil, runner)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Run(ctx, statePath)
	}()

	waitForCalls(t, runner, 1)

	calls := runner.GetCalls()
	if calls[0].Args[1] != "TCP:town-os-package--core-nginx-1.0:80" {
		t.Fatalf("expected TCP:town-os-package--core-nginx-1.0:80, got %s", calls[0].Args[1])
	}

	cancel()
	<-errCh
}

func writeState(t *testing.T, path string, state PackageNetworkState) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
