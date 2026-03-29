package upnp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
)

// Manager defines the interface for uPnP port mapping operations.
type Manager interface {
	AddPortMapping(protocol string, externalPort, internalPort uint16, description string, ttl uint32) error
	RemovePortMapping(protocol string, externalPort uint16) error
}

// IGDClient discovers an Internet Gateway Device and performs uPnP port
// mapping operations against it.
type IGDClient struct {
	client  *internetgateway2.WANIPConnection2
	localIP string
}

// NewIGDClient discovers an IGD on the local network and returns a client
// ready to manage port mappings.
func NewIGDClient() (_ *IGDClient, err error) {
	clients, _, err := internetgateway2.NewWANIPConnection2Clients()
	if err != nil {
		return nil, fmt.Errorf("discover IGD: %w", err)
	}
	if len(clients) == 0 {
		return nil, errors.New("no IGD device found")
	}

	localIP, err := localAddress()
	if err != nil {
		return nil, fmt.Errorf("local address: %w", err)
	}

	return &IGDClient{client: clients[0], localIP: localIP}, nil
}

func localAddress() (_ string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer func() {
		cerr := conn.Close()
		if cerr != nil && err == nil {
			err = cerr
		}
	}()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected address type: %T", conn.LocalAddr())
	}
	return addr.IP.String(), nil
}

// AddPortMapping creates a port mapping on the IGD.
func (c *IGDClient) AddPortMapping(protocol string, externalPort, internalPort uint16, description string, ttl uint32) error {
	return c.client.AddPortMapping(
		"",
		externalPort,
		protocol,
		internalPort,
		c.localIP,
		true,
		description,
		ttl,
	)
}

// RemovePortMapping removes a port mapping from the IGD.
func (c *IGDClient) RemovePortMapping(protocol string, externalPort uint16) error {
	return c.client.DeletePortMapping(
		"",
		externalPort,
		protocol,
	)
}

// MockCall records a single method invocation on MockManager.
type MockCall struct {
	Method string
	Args   []any
}

// MockManager is a test double for Manager that records calls and supports
// injectable errors.
type MockManager struct {
	mu    sync.Mutex
	Calls []MockCall

	AddErr    error
	RemoveErr error
}

// GetCalls returns a copy of the recorded calls.
func (m *MockManager) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

// AddPortMapping records the call and returns any injected error.
func (m *MockManager) AddPortMapping(protocol string, externalPort, internalPort uint16, description string, ttl uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{
		Method: "AddPortMapping",
		Args:   []any{protocol, externalPort, internalPort, description, ttl},
	})
	return m.AddErr
}

// RemovePortMapping records the call and returns any injected error.
func (m *MockManager) RemovePortMapping(protocol string, externalPort uint16) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{
		Method: "RemovePortMapping",
		Args:   []any{protocol, externalPort},
	})
	return m.RemoveErr
}
