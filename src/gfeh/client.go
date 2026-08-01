// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is the systemcontroller's view of one gfehd administrative surface.
//
// There is no token and no credential: gfehd binds this to a Unix socket and
// never to a port, so the boundary between "an administrator on this machine"
// and "anyone who can reach the network" is a routing property rather than a
// conditional in a handler. Filesystem permissions on the socket are the whole
// access control, which is why the systemcontroller is the only caller and why
// the UI reaches it exclusively through authenticated /gfeh/* proxy routes.
type Client interface {
	Health(ctx context.Context) (Health, error)
	Names(ctx context.Context) (NameList, error)

	ListPrincipals(ctx context.Context) ([]Principal, error)
	CreatePrincipal(ctx context.Context, p Principal) (Principal, error)
	DeletePrincipal(ctx context.Context, name string) error

	// ListGrants requires a principal. gfehd's handler takes it as a required
	// query parameter with no default, so an absent one is a 4xx rather than
	// "every grant".
	ListGrants(ctx context.Context, principal string) ([]Grant, error)
	CreateGrant(ctx context.Context, g Grant) (Grant, error)
	RevokeGrant(ctx context.Context, id int64) error

	ListExposures(ctx context.Context) ([]Exposure, error)
	WithdrawExposure(ctx context.Context, token string) error
}

// Errors callers branch on. gfehd maps its internal errors onto HTTP status
// codes (404 not found, 409 already exists, 400 malformed), and these are the
// Go side of that mapping.
var (
	ErrNotFound      = errors.New("gfeh: not found")
	ErrAlreadyExists = errors.New("gfeh: already exists")
	ErrBadRequest    = errors.New("gfeh: bad request")
	ErrUnavailable   = errors.New("gfeh: daemon unavailable")
)

// StatusError carries the status and message gfehd reported, for the cases a
// caller wants to surface rather than classify.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("gfeh: %s (status %d)", e.Message, e.Status)
}

// Unwrap maps the status onto a sentinel so errors.Is works.
func (e *StatusError) Unwrap() error {
	switch e.Status {
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrAlreadyExists
	case http.StatusBadRequest:
		return ErrBadRequest
	case http.StatusServiceUnavailable:
		return ErrUnavailable
	}
	return nil
}

// defaultTimeout bounds every admin call. The socket is local and gfehd's
// handlers are index lookups, so a request that has not answered in this long
// is a daemon in trouble; blocking a reconcile on it would hold up DNS and the
// ingress for every other partition.
const defaultTimeout = 10 * time.Second

// UnixClient talks to gfehd over its admin socket.
type UnixClient struct {
	socket string
	http   *http.Client
}

// NewClient builds a client for the socket at path.
//
// It never fails and never dials: constructing the transport is enough, and a
// daemon that is not up yet simply produces an error on the first call. This is
// deliberately unlike rolodex.Dial and ingress.Dial, which are gRPC and have to
// establish a connection — a constructor that could fail here would force every
// caller to handle a startup race that resolves itself.
func NewClient(socket string) *UnixClient {
	return &UnixClient{
		socket: socket,
		http: &http.Client{
			Timeout: defaultTimeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socket)
				},
			},
		},
	}
}

// SocketPath returns the socket this client talks to.
func (c *UnixClient) SocketPath() string { return c.socket }

// url builds a request URL. The host is a placeholder: the transport ignores
// it and dials the socket regardless, but net/http still requires a valid one.
func (c *UnixClient) url(path string) string { return "http://gfeh" + path }

// do issues a request and decodes the response into out (which may be nil).
//
// The return is named so the deferred body close can join its error into it
// rather than dropping it.
func (c *UnixClient) do(ctx context.Context, method, path string, body, out any) (err error) {
	var reader io.Reader
	if body != nil {
		encoded, encErr := json.Marshal(body)
		if encErr != nil {
			return fmt.Errorf("gfeh: encode %s %s: %w", method, path, encErr)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url(path), reader)
	if err != nil {
		return fmt.Errorf("gfeh: build %s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, doErr := c.http.Do(req)
	if doErr != nil {
		// A dial failure is the ordinary "daemon not up yet" case, not a bug.
		return fmt.Errorf("%w: %s %s: %w", ErrUnavailable, method, path, doErr)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &StatusError{Status: resp.StatusCode, Message: decodeError(resp.Body)}
	}

	if out == nil {
		return nil
	}
	if decErr := json.NewDecoder(resp.Body).Decode(out); decErr != nil {
		return fmt.Errorf("gfeh: decode %s %s: %w", method, path, decErr)
	}
	return nil
}

// decodeError pulls gfehd's {"error": "..."} out of a failure response,
// falling back to whatever the body held.
func decodeError(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, 4096))
	if err != nil || len(raw) == 0 {
		return "request failed"
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Error != "" {
		return payload.Error
	}
	return strings.TrimSpace(string(raw))
}

// Health reports the daemon's status and which partition it serves.
func (c *UnixClient) Health(ctx context.Context) (Health, error) {
	var out Health
	err := c.do(ctx, http.MethodGet, "/v1/health", nil, &out)
	return out, err
}

// Names asks the daemon which hostnames Town OS should publish for it.
//
// This is the route the whole "gfeh contains no DNS client" rule rests on:
// Town OS asks on every reconcile and folds the answer into what it is about
// to derive, so there is no code path by which gfeh could clobber a record
// RebuildDNS owns.
func (c *UnixClient) Names(ctx context.Context) (NameList, error) {
	var out NameList
	err := c.do(ctx, http.MethodGet, "/v1/names", nil, &out)
	return out, err
}

// ListPrincipals returns the partition's ACL forest, roots first.
func (c *UnixClient) ListPrincipals(ctx context.Context) ([]Principal, error) {
	out := []Principal{}
	err := c.do(ctx, http.MethodGet, "/v1/principals", nil, &out)
	return out, err
}

// CreatePrincipal projects an account into the partition. Idempotent from the
// caller's point of view only in the sense that a duplicate answers 409;
// callers that re-run on every reconcile should treat ErrAlreadyExists as
// success.
func (c *UnixClient) CreatePrincipal(ctx context.Context, p Principal) (Principal, error) {
	var out Principal
	err := c.do(ctx, http.MethodPost, "/v1/principals", p, &out)
	return out, err
}

// DeletePrincipal removes a principal and, per gfeh's schema, the grants
// derived from it.
func (c *UnixClient) DeletePrincipal(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/principals/"+url.PathEscape(name), nil, nil)
}

// ListGrants returns everything one principal holds.
func (c *UnixClient) ListGrants(ctx context.Context, principal string) ([]Grant, error) {
	out := []Grant{}
	path := "/v1/grants?principal=" + url.QueryEscape(principal)
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// CreateGrant grants a principal authority over a subtree. The returned grant
// carries the rights as stored — clamped to the principal's ceiling — which is
// not necessarily what was asked for.
func (c *UnixClient) CreateGrant(ctx context.Context, g Grant) (Grant, error) {
	var out Grant
	err := c.do(ctx, http.MethodPost, "/v1/grants", g, &out)
	return out, err
}

// RevokeGrant removes a grant by row id.
func (c *UnixClient) RevokeGrant(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/v1/grants/%d", id), nil, nil)
}

// ListExposures returns the partition's published /f/<token> links.
func (c *UnixClient) ListExposures(ctx context.Context) ([]Exposure, error) {
	out := []Exposure{}
	err := c.do(ctx, http.MethodGet, "/v1/exposures", nil, &out)
	return out, err
}

// WithdrawExposure stops a published link from resolving.
func (c *UnixClient) WithdrawExposure(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodDelete, "/v1/exposures/"+url.PathEscape(token), nil, nil)
}
