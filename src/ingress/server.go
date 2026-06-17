// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

// Package ingress is the shared :443 SNI ingress: a sidecar that supervises a
// caddy child and exposes a gRPC management API the systemcontroller programs
// (the same way it programs rolodex). It holds the desired route set in memory,
// renders a Caddyfile on every change, and reloads caddy zero-downtime.
package ingress

import (
	"context"
	"sync"

	"gitea.com/town-os/town-os/src/caddysup"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// Server implements the Ingress gRPC service. It owns the route set and the
// caddy supervisor; every mutation re-renders the Caddyfile and reloads caddy.
type Server struct {
	ingresspb.UnimplementedIngressServer

	mu        sync.Mutex
	routes    map[string]*ingresspb.Route // keyed by hostname
	sup       caddysup.CaddySupervisor
	httpsPort int
}

// NewServer returns an ingress Server backed by the given caddy supervisor.
// httpsPort is the TCP port the rendered vhosts bind (443 in production; an
// ephemeral port in tests). A value of 0 is treated as 443.
func NewServer(sup caddysup.CaddySupervisor, httpsPort int) *Server {
	return &Server{
		routes:    make(map[string]*ingresspb.Route),
		sup:       sup,
		httpsPort: httpsPort,
	}
}

// Bootstrap renders the current (initially empty) route set and starts the
// caddy child, so the supervisor is live before the first route arrives. Safe
// to call once at startup.
func (s *Server) Bootstrap() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyLocked()
}

// applyLocked renders the current route set and reloads caddy. The caller must
// hold s.mu. Reload is a no-op when the rendered bytes are unchanged.
func (s *Server) applyLocked() error {
	list := make([]*ingresspb.Route, 0, len(s.routes))
	for _, r := range s.routes {
		list = append(list, r)
	}
	return s.sup.Reload(renderCaddyfile(list, s.httpsPort))
}

// SetRoutes replaces the entire route set (idempotent reconcile).
func (s *Server) SetRoutes(_ context.Context, req *ingresspb.SetRoutesRequest) (*ingresspb.SetRoutesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes = make(map[string]*ingresspb.Route, len(req.GetRoutes()))
	for _, r := range req.GetRoutes() {
		if r.GetHostname() != "" {
			s.routes[r.GetHostname()] = r
		}
	}
	if err := s.applyLocked(); err != nil {
		return nil, err
	}
	return &ingresspb.SetRoutesResponse{}, nil
}

// AddRoute upserts a single route keyed by hostname.
func (s *Server) AddRoute(_ context.Context, req *ingresspb.AddRouteRequest) (*ingresspb.AddRouteResponse, error) {
	r := req.GetRoute()
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.GetHostname() != "" {
		s.routes[r.GetHostname()] = r
	}
	if err := s.applyLocked(); err != nil {
		return nil, err
	}
	return &ingresspb.AddRouteResponse{}, nil
}

// RemoveRoute deletes the route for the given hostname (no error if absent).
func (s *Server) RemoveRoute(_ context.Context, req *ingresspb.RemoveRouteRequest) (*ingresspb.RemoveRouteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.routes, req.GetHostname())
	if err := s.applyLocked(); err != nil {
		return nil, err
	}
	return &ingresspb.RemoveRouteResponse{}, nil
}

// ListRoutes returns the current route set (for debugging and tests).
func (s *Server) ListRoutes(_ context.Context, _ *ingresspb.ListRoutesRequest) (*ingresspb.ListRoutesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]*ingresspb.Route, 0, len(s.routes))
	for _, r := range s.routes {
		list = append(list, r)
	}
	return &ingresspb.ListRoutesResponse{Routes: list}, nil
}
