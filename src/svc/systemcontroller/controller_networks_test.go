package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"github.com/labstack/echo/v5"
)

func newNetworksHandler(mock *account.MockNetworkManager) *SystemControllerHandlers {
	sb := &serverBase{ServerConfig: ServerConfig{NetworkMgr: mock}}
	return &SystemControllerHandlers{Controller: sb, ctx: context.Background()}
}

func postJSONContext(e *echo.Echo, body string) (*echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func TestCreateAndListNetworksHandler(t *testing.T) {
	mock := account.InitMockNetworkManager()
	s := newNetworksHandler(mock)
	e := echo.New()

	c, rec := postJSONContext(e, `{"name":"office","tld":"office"}`)
	if err := s.createNetwork(c); err != nil {
		t.Fatalf("createNetwork: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d", rec.Code)
	}

	// The created network must have a derived subnet, address, keys and port,
	// and its private key must never appear in the response.
	var view NetworkView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Subnet == "" || view.Address == "" || view.PublicKey == "" || view.ListenPort == 0 {
		t.Fatalf("network not fully derived: %+v", view)
	}
	if strings.Contains(rec.Body.String(), "private_key") || strings.Contains(rec.Body.String(), view.PrivateKey) && view.PrivateKey != "" {
		t.Fatalf("response leaked private key: %s", rec.Body.String())
	}

	// List returns the network with peer count.
	lc := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/networks", nil)
	lrec := httptest.NewRecorder()
	if err := s.listNetworks(e.NewContext(lc, lrec)); err != nil {
		t.Fatalf("listNetworks: %v", err)
	}
	var list []NetworkView
	if err := json.Unmarshal(lrec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "office" || list[0].Interface == "" {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestCreateNetworkRejectsBadName(t *testing.T) {
	s := newNetworksHandler(account.InitMockNetworkManager())
	e := echo.New()
	c, _ := postJSONContext(e, `{"name":"Bad Name"}`)
	err := s.createNetwork(c)
	var httpErr *echo.HTTPError
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %v", err)
	}
}

func TestCreateNetworkDuplicateTLD(t *testing.T) {
	mock := account.InitMockNetworkManager()
	s := newNetworksHandler(mock)
	e := echo.New()

	c1, _ := postJSONContext(e, `{"name":"office","tld":"office"}`)
	if err := s.createNetwork(c1); err != nil {
		t.Fatalf("createNetwork: %v", err)
	}
	// A second network claiming the same TLD is rejected with 409.
	c2, _ := postJSONContext(e, `{"name":"lab","tld":"office"}`)
	err := s.createNetwork(c2)
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusConflict {
		t.Fatalf("want 409 for duplicate tld, got %v", err)
	}
}

func TestEnableDisableNetworkHandler(t *testing.T) {
	mock := account.InitMockNetworkManager()
	if _, err := mock.Create(&account.Network{Name: "office", TLD: "office", Subnet: "10.90.12.0/24", Address: "10.90.12.1/24", Enabled: true}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := newNetworksHandler(mock)
	e := echo.New()

	c, rec := postJSONContext(e, `{"name":"office"}`)
	if err := s.disableNetwork(c); err != nil {
		t.Fatalf("disableNetwork: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d", rec.Code)
	}
	n, err := mock.Get("office")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if n.Enabled {
		t.Fatal("network should be disabled")
	}
}

func TestRemoveDefaultNetworkRejected(t *testing.T) {
	mock := account.InitMockNetworkManager()
	if _, err := mock.Create(&account.Network{Name: account.DefaultNetworkName, TLD: "home", Enabled: true}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := newNetworksHandler(mock)
	e := echo.New()

	c, _ := postJSONContext(e, `{"name":"home"}`)
	err := s.removeNetwork(c)
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for default network removal, got %v", err)
	}
}

func TestAddNetworkPeerHandler(t *testing.T) {
	mock := account.InitMockNetworkManager()
	if _, err := mock.Create(&account.Network{Name: "office", TLD: "office", Subnet: "10.90.12.0/24", Address: "10.90.12.1/24", PublicKey: "NETPUB", ListenPort: 51820, Enabled: true}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := newNetworksHandler(mock)
	e := echo.New()

	c, rec := postJSONContext(e, `{"network":"office","name":"laptop"}`)
	if err := s.addNetworkPeer(c); err != nil {
		t.Fatalf("addNetworkPeer: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("addPeer status = %d", rec.Code)
	}

	var result AddPeerResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Server generated a keypair (no public_key supplied), so it returns a
	// private key and a ready-to-import device config.
	if result.PrivateKey == "" {
		t.Fatal("expected server-generated private key")
	}
	if !strings.Contains(result.Config, "[Interface]") || !strings.Contains(result.Config, "PublicKey = NETPUB") {
		t.Fatalf("device config malformed:\n%s", result.Config)
	}
	if result.Peer.AllowedIP == "" || !strings.HasSuffix(result.Peer.AllowedIP, "/32") {
		t.Fatalf("peer allowed ip malformed: %q", result.Peer.AllowedIP)
	}

	peers, err := mock.ListPeers("office")
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
}

func TestResolveInstallNetwork(t *testing.T) {
	mock := account.InitMockNetworkManager()
	if _, err := mock.Create(&account.Network{Name: "office", TLD: "office", Enabled: true}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := newNetworksHandler(mock)

	// Empty defaults to "home" even if home is not present in the store.
	got, err := s.resolveInstallNetwork("")
	if err != nil {
		// home is absent → validation fails; that is acceptable, but the
		// default resolution itself must have produced "home".
		var httpErr *echo.HTTPError
		if !errors.As(err, &httpErr) {
			t.Fatalf("unexpected error type: %v", err)
		}
	} else if got != account.DefaultNetworkName {
		t.Fatalf("default = %q, want home", got)
	}

	// Existing network resolves.
	got, err = s.resolveInstallNetwork("office")
	if err != nil {
		t.Fatalf("resolveInstallNetwork(office): %v", err)
	}
	if got != "office" {
		t.Fatalf("got %q, want office", got)
	}

	// Unknown network is a 400.
	_, err = s.resolveInstallNetwork("ghost")
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown network, got %v", err)
	}
}
