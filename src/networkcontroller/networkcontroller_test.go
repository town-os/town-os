package networkcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		Repo:        "core",
		Package:     "nginx",
		Version:     "1.0",
		NetworkMode: "host",
		Ports: []PortConfig{
			{ExternalPort: 8080, InternalPort: 80, UPnP: true, Forward: true},
		},
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
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
	if parsed.NetworkMode != "host" {
		t.Fatalf("expected network_mode host, got %s", parsed.NetworkMode)
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
	ctrl := NewControllerWithRunner(mock, runner)

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
	if calls[0].Args[1] != "TCP:127.0.0.1:80" {
		t.Fatalf("expected TCP:127.0.0.1:80, got %s", calls[0].Args[1])
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
	ctrl := NewControllerWithRunner(mock, runner)

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
	ctrl := NewControllerWithRunner(mock, runner)

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
	ctrl := NewControllerWithRunner(mock, runner)

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

	desc := upnpCalls[0].Args[3].(string)
	expected := "Town OS: Forward for nginx@1.0 on 8080"
	if desc != expected {
		t.Fatalf("expected description %q, got %q", expected, desc)
	}
}

func TestUPnPPortLogicForwardTrue(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunner(mock, runner)

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
	extPort := upnpCalls[0].Args[1].(uint16)
	intPort := upnpCalls[0].Args[2].(uint16)
	if extPort != 8080 || intPort != 8080 {
		t.Fatalf("expected AddPortMapping(8080, 8080), got (%d, %d)", extPort, intPort)
	}
}

func TestUPnPPortLogicForwardFalse(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunner(mock, runner)

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
	extPort := upnpCalls[0].Args[1].(uint16)
	intPort := upnpCalls[0].Args[2].(uint16)
	if extPort != 8080 || intPort != 80 {
		t.Fatalf("expected AddPortMapping(8080, 80), got (%d, %d)", extPort, intPort)
	}
}

func TestPeriodicRenewal(t *testing.T) {
	mock := &upnp.MockManager{}
	runner := newMockRunner()
	ctrl := NewControllerWithRunner(mock, runner)

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
	ctrl := NewControllerWithRunner(mock, runner)

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
	ctrl := NewControllerWithRunner(mock, runner)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ctrl.Run(ctx, path)
	}()

	// Wait for initial reconcile.
	time.Sleep(200 * time.Millisecond)

	if len(runner.GetCalls()) != 1 {
		t.Fatalf("expected 1 exec call after initial reconcile, got %d", len(runner.GetCalls()))
	}

	// Modify state file.
	state.Ports = append(state.Ports, PortConfig{ExternalPort: 8443, InternalPort: 443, UPnP: false, Forward: true})
	writeState(t, path, state)

	// Wait for fsnotify to trigger reconcile.
	time.Sleep(500 * time.Millisecond)

	if len(runner.GetCalls()) != 2 {
		t.Fatalf("expected 2 exec calls after file change, got %d", len(runner.GetCalls()))
	}

	cancel()
	<-errCh
}

func TestUPnPUnavailableDoesNotCrash(t *testing.T) {
	runner := newMockRunner()
	// nil UPnP manager = UPnP unavailable.
	ctrl := NewControllerWithRunner(nil, runner)

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
	ctrl := NewControllerWithRunner(mock, runner)

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
	mock := &upnp.MockManager{AddErr: fmt.Errorf("simulated UPnP error")}
	runner := newMockRunner()
	ctrl := NewControllerWithRunner(mock, runner)

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

func writeState(t *testing.T, path string, state PackageNetworkState) {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
