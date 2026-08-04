// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"context"
	"sort"
	"sync"
)

// MockClient is a thread-safe in-memory Client for tests that need a partition
// to answer without podman, systemd, or a real gfehd.
//
// It is deliberately more than a stub recorder: the DNS and ingress collectors
// are the code most worth testing here, and they only do anything interesting
// against a client that returns a plausible name list.
type MockClient struct {
	mu sync.Mutex

	HealthValue Health
	NamesValue  NameList

	Principals []Principal
	Grants     []Grant
	Exposures  []Exposure

	// Errors, when set for a method name, are returned instead of the value.
	// Keyed by method so a test can make exactly one call fail — for example
	// to prove that a partition whose socket is dead contributes no DNS
	// records rather than taking the whole reconcile down.
	Errors map[string]error

	// GrantClamp narrows a requested permission set the way gfehd narrows one
	// against the principal's ceiling. Nil stores the grant exactly as asked.
	//
	// A hook rather than a table, because the table is gfehd's and belongs to
	// gfeh's own suite: reimplementing it here would let this mock agree with
	// a rule Town OS invented and disagree with the daemon. What Town OS is
	// responsible for — and what a test using this proves — is reporting the
	// perms that came *back* rather than echoing the ones it sent, so an
	// administrator can see that a grant was narrowed instead of believing
	// they handed out access nobody has.
	GrantClamp func([]string) []string

	// Calls records method names in order.
	Calls []string
}

// NewMockClient returns a mock answering for one partition on one network.
func NewMockClient(partition, network string, names ...Name) *MockClient {
	m := &MockClient{
		HealthValue: Health{Status: "ok", Partition: partition},
		NamesValue:  NameList{Partition: partition, Names: names},
		Errors:      map[string]error{},
	}
	if network != "" {
		n := network
		m.NamesValue.Network = &n
	}
	return m
}

func (m *MockClient) record(method string) error {
	m.Calls = append(m.Calls, method)
	if err, ok := m.Errors[method]; ok {
		return err
	}
	return nil
}

// CallsFor counts how many times a method was invoked.
func (m *MockClient) CallsFor(method string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	n := 0
	for _, c := range m.Calls {
		if c == method {
			n++
		}
	}
	return n
}

func (m *MockClient) Health(_ context.Context) (Health, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.record("Health"); err != nil {
		return Health{}, err
	}
	return m.HealthValue, nil
}

func (m *MockClient) Names(_ context.Context) (NameList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.record("Names"); err != nil {
		return NameList{}, err
	}
	return m.NamesValue, nil
}

func (m *MockClient) ListPrincipals(_ context.Context) ([]Principal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.record("ListPrincipals"); err != nil {
		return nil, err
	}
	out := make([]Principal, len(m.Principals))
	copy(out, m.Principals)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MockClient) CreatePrincipal(_ context.Context, p Principal) (Principal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.record("CreatePrincipal"); err != nil {
		return Principal{}, err
	}
	for _, existing := range m.Principals {
		if existing.Name == p.Name {
			return Principal{}, &StatusError{Status: 409, Message: "principal already exists"}
		}
	}
	m.Principals = append(m.Principals, p)
	return p, nil
}

func (m *MockClient) DeletePrincipal(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.record("DeletePrincipal"); err != nil {
		return err
	}
	for i, existing := range m.Principals {
		if existing.Name == name {
			m.Principals = append(m.Principals[:i], m.Principals[i+1:]...)
			// Deleting a principal takes its grants with it, as gfeh's schema
			// does; a mock that left them would hide a real cleanup bug.
			kept := m.Grants[:0]
			for _, g := range m.Grants {
				if g.Principal != name {
					kept = append(kept, g)
				}
			}
			m.Grants = kept
			return nil
		}
	}
	return &StatusError{Status: 404, Message: "no such principal"}
}

func (m *MockClient) ListGrants(_ context.Context, principal string) ([]Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.record("ListGrants"); err != nil {
		return nil, err
	}
	out := []Grant{}
	for _, g := range m.Grants {
		if g.Principal == principal {
			out = append(out, g)
		}
	}
	return out, nil
}

func (m *MockClient) CreateGrant(_ context.Context, g Grant) (Grant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.record("CreateGrant"); err != nil {
		return Grant{}, err
	}
	if m.GrantClamp != nil {
		g.Perm = m.GrantClamp(g.Perm)
	}
	g.ID = int64(len(m.Grants) + 1)
	m.Grants = append(m.Grants, g)
	return g, nil
}

func (m *MockClient) RevokeGrant(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.record("RevokeGrant"); err != nil {
		return err
	}
	for i, g := range m.Grants {
		if g.ID == id {
			m.Grants = append(m.Grants[:i], m.Grants[i+1:]...)
			return nil
		}
	}
	return &StatusError{Status: 404, Message: "no such grant"}
}

func (m *MockClient) ListExposures(_ context.Context) ([]Exposure, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.record("ListExposures"); err != nil {
		return nil, err
	}
	out := make([]Exposure, len(m.Exposures))
	copy(out, m.Exposures)
	return out, nil
}

func (m *MockClient) WithdrawExposure(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.record("WithdrawExposure"); err != nil {
		return err
	}
	for i, e := range m.Exposures {
		if e.Token == token {
			m.Exposures = append(m.Exposures[:i], m.Exposures[i+1:]...)
			return nil
		}
	}
	return &StatusError{Status: 404, Message: "no such exposure"}
}
