// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package gfeh

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func baseConfig() Config {
	return Config{
		DataDir:     ContainerDataDir,
		Partition:   "home",
		AdminSocket: ContainerRunDir + "/" + AdminSocketName,
	}
}

// TestRenderConfigOmitsAnAbsentNetwork is the distinction the contract is
// explicit about: network absent means "the default", and an empty string would
// be a request to publish under a zone called "". Emitting `network: ""` would
// be a different request, not a tidier spelling of the same one.
func TestRenderConfigOmitsAnAbsentNetwork(t *testing.T) {
	out, err := RenderConfig(baseConfig())
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if strings.Contains(string(out), "network:") {
		t.Errorf("rendered a network key for an unset network:\n%s", out)
	}
}

func TestRenderConfigEmitsANamedNetwork(t *testing.T) {
	cfg := baseConfig()
	office := "office"
	cfg.Network = &office

	out, err := RenderConfig(cfg)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["network"] != "office" {
		t.Errorf("network = %v, want office", parsed["network"])
	}
}

// TestValidateRejectsAnEmptyNetwork: a set-but-empty network is the one spelling
// that is neither "the default" nor a real zone, and gfehd would accept it.
func TestValidateRejectsAnEmptyNetwork(t *testing.T) {
	cfg := baseConfig()
	empty := ""
	cfg.Network = &empty

	if _, err := RenderConfig(cfg); err == nil {
		t.Fatal("rendered a config with a set-but-empty network")
	}
}

// TestRenderConfigOmitsUnservedViews: a view with no bind address is a view
// that is not served, and that absence is how a deployment turns one off.
// Emitting `s3: null` is not the same thing — gfehd would try to parse it as a
// section and fail on the missing required bind.
func TestRenderConfigOmitsUnservedViews(t *testing.T) {
	out, err := RenderConfig(baseConfig())
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	for _, key := range []string{"s3:", "http:", "drive:", "ipfs:", "smb:", "town_os:", "credentials:"} {
		if strings.Contains(string(out), key) {
			t.Errorf("rendered %q for an unconfigured section:\n%s", key, out)
		}
	}
}

// TestRenderConfigIsDeterministic is what lets reconcile compare the rendered
// bytes against what is on disk and skip restarting a daemon whose config has
// not actually changed. A render that varied would bounce every partition on
// every reconcile.
func TestRenderConfigIsDeterministic(t *testing.T) {
	cfg := baseConfig()
	cfg.S3 = &S3Config{Bind: "0.0.0.0:9000", Region: "us-east-1", Hostname: "s3.gfeh"}
	cfg.HTTP = &HTTPConfig{Bind: "0.0.0.0:9001", Hostname: "http.gfeh"}
	cfg.Credentials = []CredentialConfig{{AccessKey: "AKIA", Secret: "shh", Principal: "alice"}}

	first, err := RenderConfig(cfg)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	for i := range 5 {
		again, err := RenderConfig(cfg)
		if err != nil {
			t.Fatalf("RenderConfig: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("render %d differs:\nfirst:\n%s\nagain:\n%s", i, first, again)
		}
	}
}

// TestRenderConfigUsesOnlyKeysGfehdAccepts. gfehd parses with
// serde(deny_unknown_fields), so a key it does not know is a hard startup
// failure rather than a warning. This pins the top-level key set against
// crates/gfehd/src/config.rs.
func TestRenderConfigUsesOnlyKeysGfehdAccepts(t *testing.T) {
	cfg := baseConfig()
	office := "office"
	cfg.Network = &office
	cfg.S3 = &S3Config{Bind: "0.0.0.0:9000"}
	cfg.HTTP = &HTTPConfig{Bind: "0.0.0.0:9001"}
	cfg.Drive = &DriveConfig{Bind: "0.0.0.0:9002"}
	cfg.IPFS = &IPFSConfig{Bind: "0.0.0.0:9003"}
	cfg.SMB = &SMBConfig{Bind: "0.0.0.0:4450", Share: "gfeh", Principal: "smb"}
	cfg.Credentials = []CredentialConfig{{AccessKey: "AKIA", Secret: "shh", Principal: "alice"}}

	out, err := RenderConfig(cfg)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	allowed := map[string]bool{
		"data_dir": true, "partition": true, "network": true, "admin_socket": true,
		"s3": true, "http": true, "drive": true, "ipfs": true, "smb": true,
		"credentials": true, "town_os": true,
	}
	for key := range parsed {
		if !allowed[key] {
			t.Errorf("rendered key %q, which gfehd's deny_unknown_fields would refuse", key)
		}
	}
}

func TestValidateRejectsAPartitionWithASeparator(t *testing.T) {
	cfg := baseConfig()
	cfg.Partition = "../../etc"

	if _, err := RenderConfig(cfg); err == nil {
		t.Fatal("rendered a partition name that escapes the data directory")
	}
}

func TestValidateRequiresTheBasics(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(*Config)
	}{
		{"no data_dir", func(c *Config) { c.DataDir = "" }},
		{"no admin_socket", func(c *Config) { c.AdminSocket = "" }},
		{"no partition", func(c *Config) { c.Partition = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig()
			tc.fn(&cfg)
			if _, err := RenderConfig(cfg); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

// TestValidateRejectsAnUnprincipledListener mirrors gfehd's own checks: an
// access key naming no principal authenticates to nobody, which is worse than
// being absent — the request gets past the signature check and then fails
// somewhere that looks like a permissions bug.
func TestValidateRejectsAnUnprincipledListener(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(*Config)
	}{
		{"smb with no principal", func(c *Config) {
			c.SMB = &SMBConfig{Bind: "0.0.0.0:4450", Principal: "  "}
		}},
		{"credential with no principal", func(c *Config) {
			c.Credentials = []CredentialConfig{{AccessKey: "AKIA", Secret: "shh", Principal: ""}}
		}},
		{"credential with no secret", func(c *Config) {
			c.Credentials = []CredentialConfig{{AccessKey: "AKIA", Secret: "", Principal: "alice"}}
		}},
		{"drive token with no principal", func(c *Config) {
			c.Drive = &DriveConfig{Bind: "0.0.0.0:9002", Tokens: []TokenConfig{{Token: "t", Principal: ""}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig()
			tc.fn(&cfg)
			if _, err := RenderConfig(cfg); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

// TestValidateRejectsATownOSSectionWithNoCredential: gfehd would fail at
// startup with a less obvious message, after it had already decided which
// directory to serve out of.
func TestValidateRejectsATownOSSectionWithNoCredential(t *testing.T) {
	cfg := baseConfig()
	cfg.TownOS = &TownOSConfig{BaseURL: "http://127.0.0.1:5309", Username: "root"}

	if _, err := RenderConfig(cfg); err == nil {
		t.Fatal("rendered a town_os section naming neither a password nor a token")
	}
}

// TestIsHTTPView keeps SMB out of the ingress. Contributing a vhost for
// something that does not speak HTTP produces a route that completes a TLS
// handshake and then fails, which is worse than no route at all.
func TestIsHTTPView(t *testing.T) {
	for _, v := range []string{ViewS3, ViewHTTP, ViewDrive, ViewIPFS} {
		if !IsHTTPView(v) {
			t.Errorf("IsHTTPView(%q) = false, want true", v)
		}
	}
	for _, v := range []string{ViewSMB, "", "gopher"} {
		if IsHTTPView(v) {
			t.Errorf("IsHTTPView(%q) = true, want false", v)
		}
	}
}

// TestCeilingForAccount pins gfeh's own projection rule: a Town OS admin is a
// gfeh superuser because they create the roots of the forest, and an ordinary
// account gets a ceiling and no grants — authenticating is not authorization.
func TestCeilingForAccount(t *testing.T) {
	admin := CeilingForAccount(true)
	if len(admin) != 1 || admin[0] != PermAll {
		t.Errorf("admin ceiling = %v, want [%s]", admin, PermAll)
	}
	user := CeilingForAccount(false)
	if len(user) != 1 || user[0] != PermReadWrite {
		t.Errorf("account ceiling = %v, want [%s]", user, PermReadWrite)
	}
}

// TestNameListNetworkName covers the absent-means-default rule at the one place
// Town OS converts the pointer into a network it knows.
func TestNameListNetworkName(t *testing.T) {
	if got := (NameList{}).NetworkName("home"); got != "home" {
		t.Errorf("absent network = %q, want home", got)
	}
	empty := ""
	if got := (NameList{Network: &empty}).NetworkName("home"); got != "home" {
		t.Errorf("empty network = %q, want home", got)
	}
	office := "office"
	if got := (NameList{Network: &office}).NetworkName("home"); got != "office" {
		t.Errorf("named network = %q, want office", got)
	}
}
