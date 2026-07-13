package packages

import (
	"errors"
	"testing"
)

func plexFlow() *OAuthFlow {
	return &OAuthFlow{
		Start: OAuthStep{
			Method:  "POST",
			URL:     "https://plex.tv/api/v2/pins?strong=true",
			Headers: map[string]string{"X-Plex-Client-Identifier": "{{client_id}}"},
		},
		Extract:  map[string]string{"id": "id", "code": "code"},
		Approve:  "https://app.plex.tv/auth#?clientID={{client_id}}&code={{code}}",
		Poll:     OAuthStep{URL: "https://plex.tv/api/v2/pins/{{id}}"},
		Token:    "authToken",
		Interval: "5s",
		Timeout:  "5m",
	}
}

func TestValidateOAuthFlow(t *testing.T) {
	t.Parallel()

	t.Run("a complete flow validates", func(t *testing.T) {
		t.Parallel()
		q := Question{Query: "Plex account", Type: Oauth, OAuth: plexFlow()}
		if err := ValidateOAuthFlow("plextoken", q); err != nil {
			t.Fatalf("ValidateOAuthFlow: %v", err)
		}
	})

	t.Run("oauth question without a flow is rejected", func(t *testing.T) {
		t.Parallel()
		q := Question{Query: "Plex account", Type: Oauth}
		if err := ValidateOAuthFlow("plextoken", q); !errors.Is(err, ErrInvalidOAuthSpec) {
			t.Fatalf("err = %v, want ErrInvalidOAuthSpec", err)
		}
	})

	// A flow on a plain text question would never run, and the token field would
	// read as if it were doing something. Fail the package rather than ignore it.
	t.Run("flow on a non-oauth question is rejected", func(t *testing.T) {
		t.Parallel()
		q := Question{Query: "Token", OAuth: plexFlow()}
		if err := ValidateOAuthFlow("plextoken", q); !errors.Is(err, ErrInvalidOAuthSpec) {
			t.Fatalf("err = %v, want ErrInvalidOAuthSpec", err)
		}
	})

	for name, missing := range map[string]func(f *OAuthFlow){
		"start.url": func(f *OAuthFlow) { f.Start.URL = "" },
		"approve":   func(f *OAuthFlow) { f.Approve = "" },
		"poll.url":  func(f *OAuthFlow) { f.Poll.URL = "" },
		"token":     func(f *OAuthFlow) { f.Token = "" },
	} {
		t.Run("missing "+name+" is rejected", func(t *testing.T) {
			t.Parallel()
			f := plexFlow()
			missing(f)
			q := Question{Query: "Plex account", Type: Oauth, OAuth: f}
			if err := ValidateOAuthFlow("plextoken", q); !errors.Is(err, ErrInvalidOAuthSpec) {
				t.Fatalf("err = %v, want ErrInvalidOAuthSpec", err)
			}
		})
	}

	t.Run("an unparseable duration is rejected", func(t *testing.T) {
		t.Parallel()
		f := plexFlow()
		f.Interval = "soon"
		q := Question{Query: "Plex account", Type: Oauth, OAuth: f}
		if err := ValidateOAuthFlow("plextoken", q); !errors.Is(err, ErrInvalidOAuthSpec) {
			t.Fatalf("err = %v, want ErrInvalidOAuthSpec", err)
		}
	})
}

// The system controller runs as root on the host and fetches these URLs itself,
// while the package that names them is otherwise confined to a container. Left
// unguarded, a package could use an oauth question to reach anything on the
// host's network -- so a URL must be https and must not name a private address.
func TestValidateOAuthURL(t *testing.T) {
	t.Parallel()

	for name, item := range map[string]struct {
		url     string
		allowed bool
	}{
		"public https":            {url: "https://plex.tv/api/v2/pins", allowed: true},
		"templated path":          {url: "https://plex.tv/api/v2/pins/{{id}}", allowed: true},
		"templated query":         {url: "https://plex.tv/a?code={{code}}", allowed: true},
		"plain http":              {url: "http://plex.tv/api/v2/pins"},
		"loopback literal":        {url: "https://127.0.0.1/token"},
		"loopback name":           {url: "https://localhost/token"},
		"private 10/8":            {url: "https://10.0.0.5/token"},
		"private 192.168/16":      {url: "https://192.168.122.50/token"},
		"link-local metadata":     {url: "https://169.254.169.254/latest/meta-data"},
		"ipv6 loopback":           {url: "https://[::1]/token"},
		"carrier-grade nat":       {url: "https://100.64.1.1/token"},
		"file scheme":             {url: "file:///etc/shadow"},
		"no host":                 {url: "https:///token"},
		"unix-ish nonsense":       {url: "not-a-url"},
		"templated host rejected": {url: "https://{{host}}/token"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := ValidateOAuthURL(item.url)
			if item.allowed && err != nil {
				t.Fatalf("ValidateOAuthURL(%q) = %v, want allowed", item.url, err)
			}
			if !item.allowed && err == nil {
				t.Fatalf("ValidateOAuthURL(%q) was allowed; want rejected", item.url)
			}
		})
	}
}

// The URL check cannot see where a hostname resolves to, so the dialer checks
// every address it is about to connect to. This is what stops a package naming a
// perfectly public-looking name that answers with 127.0.0.1.
func TestCheckOAuthAddr(t *testing.T) {
	t.Parallel()

	for name, item := range map[string]struct {
		addr    string
		allowed bool
	}{
		"public":              {addr: "104.18.32.1:443", allowed: true},
		"public ipv6":         {addr: "[2606:4700::1]:443", allowed: true},
		"loopback":            {addr: "127.0.0.1:443"},
		"loopback ipv6":       {addr: "[::1]:443"},
		"private":             {addr: "192.168.122.50:443"},
		"private 172.16/12":   {addr: "172.16.4.4:443"},
		"link-local":          {addr: "169.254.169.254:443"},
		"unspecified":         {addr: "0.0.0.0:443"},
		"carrier-grade nat":   {addr: "100.100.100.100:443"},
		"not an ip":           {addr: "plex.tv:443"},
		"missing port":        {addr: "104.18.32.1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := CheckOAuthAddr("tcp", item.addr)
			if item.allowed && err != nil {
				t.Fatalf("CheckOAuthAddr(%q) = %v, want allowed", item.addr, err)
			}
			if !item.allowed && err == nil {
				t.Fatalf("CheckOAuthAddr(%q) was allowed; want rejected", item.addr)
			}
		})
	}
}

// The answer to an oauth question is the token the flow returned, so it must
// validate like a secret: non-empty, passed through untouched.
func TestOauthOutputType(t *testing.T) {
	t.Parallel()

	got, err := Oauth.Output("plex-auth-token")
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if got != "plex-auth-token" {
		t.Fatalf("Output = %q, want the token unchanged", got)
	}
	if _, err := Oauth.Output(""); err == nil {
		t.Fatal("an empty oauth answer should be rejected: the flow was never completed")
	}
}

// The split that matters. A package is compiled and installed long after its
// flow ran, and possibly on a host that legitimately allows a private provider
// (a test's httptest server, a self-hosted identity server on the LAN). The
// address rules therefore belong to the host running the flow, not to the
// package sitting in the repository -- so the spec check has to accept a URL the
// flow check refuses to dial. Fusing the two is what made an install fail after
// its own flow had already succeeded.
func TestValidateOAuthSpecIgnoresAddressPolicy(t *testing.T) {
	t.Parallel()

	f := plexFlow()
	f.Start.URL = "http://127.0.0.1:8080/api/v2/pins"
	f.Poll.URL = "http://127.0.0.1:8080/api/v2/pins/{{id}}"
	q := Question{Query: "Plex account", Type: Oauth, OAuth: f}

	if err := ValidateOAuthSpec("plextoken", q); err != nil {
		t.Fatalf("ValidateOAuthSpec rejected a well-formed flow over its address: %v", err)
	}
	if err := ValidateOAuthFlow("plextoken", q); !errors.Is(err, ErrInvalidOAuthSpec) {
		t.Fatalf("ValidateOAuthFlow allowed a loopback provider: %v", err)
	}
}

// A malformed flow is a package bug whichever way you look at it, so the spec
// check still has to catch what does not depend on the host.
func TestValidateOAuthSpecRejectsTemplatedHost(t *testing.T) {
	t.Parallel()

	f := plexFlow()
	f.Poll.URL = "https://{{host}}/api/v2/pins/{{id}}"
	q := Question{Query: "Plex account", Type: Oauth, OAuth: f}

	if err := ValidateOAuthSpec("plextoken", q); !errors.Is(err, ErrInvalidOAuthSpec) {
		t.Fatalf("err = %v, want ErrInvalidOAuthSpec", err)
	}
}
