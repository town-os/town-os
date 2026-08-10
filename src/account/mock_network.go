package account

import (
	"context"
	"sort"
	"sync"
	"time"
)

type MockNetworkManager struct {
	mu       sync.Mutex
	networks map[string]*Network
	peers    map[string][]*NetworkPeer
	Calls    []MockCall

	CreateErr           error
	GetErr              error
	ListErr             error
	RemoveErr           error
	SetEnabledErr       error
	SetTLDErr           error
	AddPeerErr          error
	RemovePeerErr       error
	ListPeersErr        error
	RefreshPeerErr      error
	ReapExpiredPeersErr error
}

// InitMockNetworkManager builds an empty mock, carrying the home network the
// SQLite manager seeds. A mock that started with no networks would let a test
// pass against a state the real manager cannot be in -- the home network always
// exists -- and every caller that resolves the default network would take its
// not-found branch only here.
func InitMockNetworkManager() *MockNetworkManager {
	m := &MockNetworkManager{
		networks: map[string]*Network{},
		peers:    map[string][]*NetworkPeer{},
	}
	// Seeded through the map rather than through Create, so it does not appear
	// in Calls: a test asserting on the calls it made must not have to know the
	// constructor made one of its own.
	home := DefaultNetwork()
	home.CreatedAt = time.Now()
	home.UpdatedAt = home.CreatedAt
	m.networks[DefaultNetworkName] = home
	return m
}

func (m *MockNetworkManager) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MockCall, len(m.Calls))
	copy(out, m.Calls)
	return out
}

// Seed installs a network directly, replacing any row of the same name and
// recording no call.
//
// It exists for the home network: the constructor seeds a bare one, so a test
// that wants it to carry a specific subnet, address, or TLD cannot get there
// through Create -- which would (correctly) refuse the duplicate. Seed says
// "this is the row" rather than "create this row", which is what such a fixture
// means.
func (m *MockNetworkManager) Seed(n *Network) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stored := *n
	now := time.Now()
	stored.CreatedAt = now
	stored.UpdatedAt = now
	m.networks[n.Name] = &stored
}

func (m *MockNetworkManager) Create(_ context.Context, n *Network) (*Network, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Create", Args: []any{n}})

	if m.CreateErr != nil {
		return nil, m.CreateErr
	}
	if n == nil || n.Name == "" {
		return nil, ErrNetworkNameRequired
	}
	if !ValidNetworkName(n.Name) {
		return nil, ErrNetworkNameInvalid
	}
	if _, exists := m.networks[n.Name]; exists {
		return nil, ErrDuplicateNetwork
	}

	now := time.Now()
	stored := *n
	stored.CreatedAt = now
	stored.UpdatedAt = now
	m.networks[n.Name] = &stored

	out := stored
	return &out, nil
}

func (m *MockNetworkManager) Get(_ context.Context, name string) (*Network, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Get", Args: []any{name}})

	if m.GetErr != nil {
		return nil, m.GetErr
	}
	n, ok := m.networks[name]
	if !ok {
		return nil, ErrNetworkNotFound
	}
	out := *n
	return &out, nil
}

func (m *MockNetworkManager) List(_ context.Context) ([]Network, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "List", Args: nil})

	if m.ListErr != nil {
		return nil, m.ListErr
	}
	out := make([]Network, 0, len(m.networks))
	for _, n := range m.networks {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MockNetworkManager) Remove(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Remove", Args: []any{name}})

	if m.RemoveErr != nil {
		return m.RemoveErr
	}
	if name == DefaultNetworkName {
		return ErrNetworkProtected
	}
	if _, ok := m.networks[name]; !ok {
		return ErrNetworkNotFound
	}
	delete(m.networks, name)
	delete(m.peers, name)
	return nil
}

func (m *MockNetworkManager) SetEnabled(_ context.Context, name string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetEnabled", Args: []any{name, enabled}})

	if m.SetEnabledErr != nil {
		return m.SetEnabledErr
	}
	n, ok := m.networks[name]
	if !ok {
		return ErrNetworkNotFound
	}
	n.Enabled = enabled
	n.UpdatedAt = time.Now()
	return nil
}

func (m *MockNetworkManager) SetTLD(_ context.Context, name, tld string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "SetTLD", Args: []any{name, tld}})

	if m.SetTLDErr != nil {
		return m.SetTLDErr
	}
	n, ok := m.networks[name]
	if !ok {
		return ErrNetworkNotFound
	}
	n.TLD = tld
	n.UpdatedAt = time.Now()
	return nil
}

func (m *MockNetworkManager) Count(_ context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "Count", Args: nil})
	return len(m.networks), nil
}

func (m *MockNetworkManager) AddPeer(_ context.Context, p *NetworkPeer) (*NetworkPeer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "AddPeer", Args: []any{p}})

	if m.AddPeerErr != nil {
		return nil, m.AddPeerErr
	}
	if p == nil || p.PublicKey == "" {
		return nil, ErrNetworkPeerKeyReq
	}
	if _, ok := m.networks[p.Network]; !ok {
		return nil, ErrNetworkNotFound
	}
	for _, existing := range m.peers[p.Network] {
		if existing.PublicKey == p.PublicKey {
			return nil, ErrDuplicateNetworkPeer
		}
	}

	stored := *p
	stored.CreatedAt = time.Now()
	m.peers[p.Network] = append(m.peers[p.Network], &stored)

	out := stored
	return &out, nil
}

func (m *MockNetworkManager) RemovePeer(_ context.Context, network, publicKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RemovePeer", Args: []any{network, publicKey}})

	if m.RemovePeerErr != nil {
		return m.RemovePeerErr
	}
	list := m.peers[network]
	for i, existing := range list {
		if existing.PublicKey == publicKey {
			m.peers[network] = append(list[:i], list[i+1:]...)
			return nil
		}
	}
	return ErrNetworkPeerNotFound
}

func (m *MockNetworkManager) RefreshPeer(_ context.Context, network, publicKey string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "RefreshPeer", Args: []any{network, publicKey, expiresAt}})

	if m.RefreshPeerErr != nil {
		return m.RefreshPeerErr
	}
	for _, p := range m.peers[network] {
		if p.PublicKey == publicKey {
			expires := expiresAt
			p.ExpiresAt = &expires
			return nil
		}
	}
	return ErrNetworkPeerNotFound
}

func (m *MockNetworkManager) ReapExpiredPeers(_ context.Context, now time.Time) ([]NetworkPeer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ReapExpiredPeers", Args: []any{now}})

	if m.ReapExpiredPeersErr != nil {
		return nil, m.ReapExpiredPeersErr
	}

	var reaped []NetworkPeer
	for network, list := range m.peers {
		kept := make([]*NetworkPeer, 0, len(list))
		for _, p := range list {
			// Non-nil expiry at or before now → reaped; nil expiry never expires.
			if p.ExpiresAt != nil && !p.ExpiresAt.After(now) {
				reaped = append(reaped, *p)
			} else {
				kept = append(kept, p)
			}
		}
		m.peers[network] = kept
	}
	sort.Slice(reaped, func(i, j int) bool {
		if reaped[i].Network != reaped[j].Network {
			return reaped[i].Network < reaped[j].Network
		}
		return reaped[i].PublicKey < reaped[j].PublicKey
	})
	return reaped, nil
}

func (m *MockNetworkManager) ListPeers(_ context.Context, network string) ([]NetworkPeer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: "ListPeers", Args: []any{network}})

	if m.ListPeersErr != nil {
		return nil, m.ListPeersErr
	}
	out := make([]NetworkPeer, 0, len(m.peers[network]))
	for _, p := range m.peers[network] {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AllowedIP != out[j].AllowedIP {
			return out[i].AllowedIP < out[j].AllowedIP
		}
		return out[i].PublicKey < out[j].PublicKey
	})
	return out, nil
}
