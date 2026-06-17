// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"context"
	"sync"

	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// MockClient is an in-memory Client for tests: it records calls and maintains
// the current route set so tests can assert what the systemcontroller
// programmed without a real ingress server. Mirrors rolodex.MockClient.
type MockClient struct {
	mu sync.Mutex

	// Routes is the current desired route set (after the last Set/Add/Remove).
	Routes []*ingresspb.Route
	// SetCalls records the routes passed to each SetRoutes call.
	SetCalls [][]*ingresspb.Route
	// Added / Removed record incremental CRUD.
	Added   []*ingresspb.Route
	Removed []string

	SetRoutesErr   error
	AddRouteErr    error
	RemoveRouteErr error
	ListRoutesErr  error
	CloseErr       error
}

// SetRoutes replaces the current route set.
func (m *MockClient) SetRoutes(_ context.Context, routes []*ingresspb.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SetCalls = append(m.SetCalls, routes)
	if m.SetRoutesErr != nil {
		return m.SetRoutesErr
	}
	m.Routes = routes
	return nil
}

// AddRoute upserts a route by hostname.
func (m *MockClient) AddRoute(_ context.Context, route *ingresspb.Route) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Added = append(m.Added, route)
	if m.AddRouteErr != nil {
		return m.AddRouteErr
	}
	for i, r := range m.Routes {
		if r.GetHostname() == route.GetHostname() {
			m.Routes[i] = route
			return nil
		}
	}
	m.Routes = append(m.Routes, route)
	return nil
}

// RemoveRoute deletes a route by hostname.
func (m *MockClient) RemoveRoute(_ context.Context, hostname string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Removed = append(m.Removed, hostname)
	if m.RemoveRouteErr != nil {
		return m.RemoveRouteErr
	}
	out := m.Routes[:0]
	for _, r := range m.Routes {
		if r.GetHostname() != hostname {
			out = append(out, r)
		}
	}
	m.Routes = out
	return nil
}

// ListRoutes returns a copy of the current route set.
func (m *MockClient) ListRoutes(_ context.Context) ([]*ingresspb.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ListRoutesErr != nil {
		return nil, m.ListRoutesErr
	}
	out := make([]*ingresspb.Route, len(m.Routes))
	copy(out, m.Routes)
	return out, nil
}

// Close is a no-op (returns CloseErr if set).
func (m *MockClient) Close() error { return m.CloseErr }

// RouteFor returns the current route for the given hostname, or nil.
func (m *MockClient) RouteFor(hostname string) *ingresspb.Route {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.Routes {
		if r.GetHostname() == hostname {
			return r
		}
	}
	return nil
}
