package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"gitea.com/town-os/town-os/src/account"
)

// ListNetworks returns all overlay networks with peer counts and running state.
func (c *SystemdClient) ListNetworks(ctx context.Context) (_ []NetworkView, err error) {
	resp, err := c.getClient(ctx, "networks")
	if err != nil {
		return nil, fmt.Errorf("%w: ListNetworks: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "networks")
	}

	var views []NetworkView
	return views, json.NewDecoder(resp.Body).Decode(&views)
}

// CreateNetwork creates an overlay network with the given name and optional TLD
// (empty TLD defaults to the name server-side) and returns the created network.
func (c *SystemdClient) CreateNetwork(ctx context.Context, name, tld string) (_ NetworkView, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, CreateNetworkRequest{Name: name, TLD: tld})

	resp, err := c.postJSON(ctx, "networks/create", pr)
	if err != nil {
		return NetworkView{}, fmt.Errorf("%w: CreateNetwork: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return NetworkView{}, readProblemDetail(resp, "POST", "networks/create")
	}

	var view NetworkView
	return view, json.NewDecoder(resp.Body).Decode(&view)
}

// RemoveNetwork deletes a network by name. The default network cannot be removed.
func (c *SystemdClient) RemoveNetwork(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, NetworkNameRequest{Name: name})
	return c.postClient(ctx, "networks/remove", pr)
}

// EnableNetwork brings a network's overlay interface up (remote access on).
func (c *SystemdClient) EnableNetwork(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, NetworkNameRequest{Name: name})
	return c.postClient(ctx, "networks/enable", pr)
}

// DisableNetwork brings a network's overlay interface down (remote access off;
// local services keep working).
func (c *SystemdClient) DisableNetwork(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, NetworkNameRequest{Name: name})
	return c.postClient(ctx, "networks/disable", pr)
}

// ListNetworkPeers returns the peers registered on the named network.
func (c *SystemdClient) ListNetworkPeers(ctx context.Context, network string) (_ []account.NetworkPeer, err error) {
	path := "networks/peers?network=" + url.QueryEscape(network)
	resp, err := c.getClient(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("%w: ListNetworkPeers: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "networks/peers")
	}

	var peers []account.NetworkPeer
	return peers, json.NewDecoder(resp.Body).Decode(&peers)
}

// AddNetworkPeer registers a peer on a network. When PublicKey is empty the
// server generates a keypair and returns the private key plus a device config.
func (c *SystemdClient) AddNetworkPeer(ctx context.Context, req AddNetworkPeerRequest) (_ AddPeerResult, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, req)

	resp, err := c.postJSON(ctx, "networks/peers/add", pr)
	if err != nil {
		return AddPeerResult{}, fmt.Errorf("%w: AddNetworkPeer: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return AddPeerResult{}, readProblemDetail(resp, "POST", "networks/peers/add")
	}

	var result AddPeerResult
	return result, json.NewDecoder(resp.Body).Decode(&result)
}

// RemoveNetworkPeer removes a peer from a network by its public key.
func (c *SystemdClient) RemoveNetworkPeer(ctx context.Context, network, publicKey string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RemoveNetworkPeerRequest{Network: network, PublicKey: publicKey})
	return c.postClient(ctx, "networks/peers/remove", pr)
}

// RefreshNetworkPeer extends a peer's TTL by the server's configured peer_ttl
// and returns the new expiry. This is the heartbeat a long-lived client issues
// to keep its enrollment from being reaped.
func (c *SystemdClient) RefreshNetworkPeer(ctx context.Context, network, publicKey string) (_ RefreshPeerResult, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RefreshNetworkPeerRequest{Network: network, PublicKey: publicKey})

	resp, err := c.postJSON(ctx, "networks/peers/refresh", pr)
	if err != nil {
		return RefreshPeerResult{}, fmt.Errorf("%w: RefreshNetworkPeer: %w", ErrHTTPRequest, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()

	if resp.StatusCode != http.StatusOK {
		return RefreshPeerResult{}, readProblemDetail(resp, "POST", "networks/peers/refresh")
	}

	var result RefreshPeerResult
	return result, json.NewDecoder(resp.Body).Decode(&result)
}
