// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

// /packages/installed/info stays requireAuth because the dashboard renders
// every installed service's notes for every account — that is what notes are
// for. The answers behind them are a different matter: a `type: secret`
// question is answered with a generated credential and a `type: oauth` one with
// a vendor token, and the handler returned the whole response map to anyone
// with a login.

const infoRedactionSecret = "d3adb33fcafebabe0123456789abcdef"

// initInfoRedactionClient builds a server with a package whose secret answer
// also appears inside one of its notes, plus real accounts so the handler has a
// caller to classify.
func initInfoRedactionClient(t *testing.T) *SystemdClient {
	t.Helper()

	db, err := account.OpenDB(filepath.Join(t.TempDir(), "info.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("db.Close: %v", cerr)
		}
	})
	mgr, err := account.InitManager(db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}

	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/core.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "core", URL: *u}}

	// The note deliberately quotes the secret: a package that publishes its
	// connection string is exactly the case where dropping the response map is
	// not enough on its own.
	writeTestPackage(t, rr.BaseDir, "core", "vault", "1.0", `image: vault:1.0
environment:
  TOKEN: "@apitoken@"
volumes: {}
questions:
  apitoken:
    query: "API token"
    type: secret
  label:
    query: "Display label"
notes:
  Connection:
    value: "https://user:@apitoken@@host.example/"
  Label:
    value: "@label@"
`)

	inst := packages.InitMockInstallManager()
	if err := inst.Install("core", "vault", "vault", "1.0", packages.Responses{
		"apitoken": infoRedactionSecret,
		"label":    "Vault",
	}); err != nil {
		t.Fatalf("seed install: %v", err)
	}

	ts := InitTestServer(ServerConfig{
		Storage:        storage.InitBtrFSMock(),
		RepositoryRoot: rr,
		Installer:      inst,
		AccountMgr:     mgr,
		SessionMgr:     sessMgr,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if _, err := c.CreateAccount(context.TODO(), "admin", authTestPassword, "admin@b.com", "555", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(context.TODO(), "admin", authTestPassword)
	if err != nil {
		t.Fatalf("Authenticate admin: %v", err)
	}
	c.Token = resp.Token

	if _, err := c.CreateAccount(context.TODO(), "alice", authTestPassword, "a@b.com", "555", "Alice", false); err != nil {
		t.Fatalf("CreateAccount alice: %v", err)
	}
	return c
}

func TestInstalledInfoWithholdsSecretsFromNonAdmin(t *testing.T) {
	c := initInfoRedactionClient(t)
	alice := authAs(t, c, "alice")

	code, body := postStatus(t, alice, "packages/installed/info", `{"repo":"core","name":"vault","version":"1.0"}`)
	if code != 200 {
		t.Fatalf("non-admin info = %d (%s), want 200", code, body)
	}
	if strings.Contains(body, infoRedactionSecret) {
		t.Fatalf("secret answer reached a non-admin: %s", body)
	}

	var info InstalledInfoResponse
	if err := json.Unmarshal([]byte(body), &info); err != nil {
		t.Fatalf("decode info: %v", err)
	}
	if len(info.Responses) != 0 {
		t.Errorf("responses = %v, want none for a non-admin", info.Responses)
	}
	if len(info.Questions) != 0 {
		t.Errorf("questions = %v, want none for a non-admin", info.Questions)
	}

	// The notes are the reason this route is not admin-only, so they have to
	// survive — with the secret masked out of the one that quoted it.
	if got := info.Notes["Label"]; got != "Vault" {
		t.Errorf("Label note = %q, want %q", got, "Vault")
	}
	conn, ok := info.Notes["Connection"]
	if !ok {
		t.Fatal("Connection note was dropped entirely")
	}
	if !strings.Contains(conn, "********") {
		t.Errorf("Connection note = %q, want the secret masked", conn)
	}
}

func TestInstalledInfoGivesAdminEverything(t *testing.T) {
	c := initInfoRedactionClient(t)

	// c carries the admin token.
	info, err := c.GetInstalledInfo(context.TODO(), "core", "vault", "1.0")
	if err != nil {
		t.Fatalf("GetInstalledInfo: %v", err)
	}
	if info.Responses["apitoken"] != infoRedactionSecret {
		t.Errorf("admin got responses %v, want the stored secret", info.Responses)
	}
	if len(info.Questions) == 0 {
		t.Error("admin got no questions")
	}
	if !strings.Contains(info.Notes["Connection"], infoRedactionSecret) {
		t.Errorf("admin's Connection note = %q, want the unmasked secret", info.Notes["Connection"])
	}
}

func TestRedactSecretsInNotes(t *testing.T) {
	questions := map[string]packages.Question{
		"apitoken": {Query: "API token", Type: packages.Secret},
		"oauthtok": {Query: "OAuth", Type: packages.Oauth},
		"port":     {Query: "Port", Type: packages.Port},
		"label":    {Query: "Label"},
	}
	responses := packages.Responses{
		"apitoken": "supersecretvalue",
		"oauthtok": "oauthtokenvalue",
		"port":     "8080",
		"label":    "Vault",
	}
	notes := map[string]string{
		"conn":  "https://user:supersecretvalue@host/",
		"oauth": "token=oauthtokenvalue",
		"plain": "listening on 8080 for Vault",
	}

	got := redactSecretsInNotes(notes, questions, responses)

	if strings.Contains(got["conn"], "supersecretvalue") {
		t.Errorf("secret survived in %q", got["conn"])
	}
	if strings.Contains(got["oauth"], "oauthtokenvalue") {
		t.Errorf("oauth token survived in %q", got["oauth"])
	}
	// A port is not a credential, and masking every short answer would shred
	// unrelated note text.
	if got["plain"] != "listening on 8080 for Vault" {
		t.Errorf("plain note = %q, want it untouched", got["plain"])
	}
}

// A package with no secret-typed questions must get its notes back by
// identity — the common case, and the one the dashboard depends on.
func TestRedactSecretsInNotesLeavesOrdinaryPackagesAlone(t *testing.T) {
	notes := map[string]string{"URL": "http://nginx.core.home:8080"}
	questions := map[string]packages.Question{"port": {Query: "Port", Type: packages.Port}}
	responses := packages.Responses{"port": "8080"}

	got := redactSecretsInNotes(notes, questions, responses)
	if got["URL"] != notes["URL"] {
		t.Errorf("note = %q, want %q", got["URL"], notes["URL"])
	}
}

// Too-short secrets are left alone deliberately: masking every occurrence of a
// two-character answer would corrupt the notes it appears inside by accident.
func TestRedactSecretsInNotesIgnoresShortAnswers(t *testing.T) {
	notes := map[string]string{"n": "port is 80 and mode is ab"}
	questions := map[string]packages.Question{"tiny": {Query: "Tiny", Type: packages.Secret}}
	responses := packages.Responses{"tiny": "ab"}

	got := redactSecretsInNotes(notes, questions, responses)
	if got["n"] != notes["n"] {
		t.Errorf("note = %q, want it untouched", got["n"])
	}
}
