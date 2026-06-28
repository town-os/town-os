package systemcontroller

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
	"github.com/labstack/echo/v5"
)

// blocklistReasonPrefix is stored as the reason on every local RBL entry that
// originates from a curated/applied blocklist feed, so the entries can be
// identified and cleared without touching operator-added local entries.
const blocklistReasonPrefix = "blocklist:"

// blocklistApplyTimeout bounds a single feed apply (fetch + parse + load).
const blocklistApplyTimeout = 15 * time.Minute

// BlocklistFeed describes a curated domain-blocklist source. The lists are
// distributed as hosts-file / plain-domain / Adblock-syntax text and are loaded
// into rolodex's local RBL list (which blocks forward domain lookups).
type BlocklistFeed struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// curatedBlocklists is the built-in catalog of recommended blocklist feeds.
var curatedBlocklists = []BlocklistFeed{
	{
		Key:         "oisd",
		Name:        "OISD",
		Description: "Widely recommended for its balance of blocking malicious sites with minimal breakage to legitimate services.",
		URL:         "https://small.oisd.nl/",
	},
	{
		Key:         "hagezi",
		Name:        "HaGeZi",
		Description: "Highly optimized blocklists that scale well for devices with memory constraints.",
		URL:         "https://raw.githubusercontent.com/hagezi/dns-blocklists/main/hosts/pro.txt",
	},
	{
		Key:         "stevenblack",
		Name:        "StevenBlack Hosts",
		Description: "A classic, clean aggregate list that covers malware, ads, and tracking.",
		URL:         "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
	},
	{
		Key:         "adguard",
		Name:        "AdGuard DNS Blocklist",
		Description: "Often utilized for non-obtrusive ad blocking at the DNS level.",
		URL:         "https://raw.githubusercontent.com/AdguardTeam/AdGuardSDNSFilter/master/Filters/filter.txt",
	},
}

func curatedBlocklistByKey(key string) (BlocklistFeed, bool) {
	for _, f := range curatedBlocklists {
		if f.Key == key {
			return f, true
		}
	}
	return BlocklistFeed{}, false
}

// blocklistFetcher fetches the raw bytes of a blocklist feed. It is a seam so
// tests can serve a small local list instead of reaching the internet.
type blocklistFetcher func(ctx context.Context, feedURL string) (io.ReadCloser, error)

// BlocklistFeedStatus reports the progress/result of applying a single feed.
type BlocklistFeedStatus struct {
	Key     string    `json:"key"`
	Added   int       `json:"added"`
	Total   int       `json:"total"`
	Done    bool      `json:"done"`
	Error   string    `json:"error,omitempty"`
	Started time.Time `json:"started"`
	Updated time.Time `json:"updated"`
}

// blocklistManager tracks the state of background blocklist applies.
type blocklistManager struct {
	mu      sync.Mutex
	running bool
	feeds   map[string]*BlocklistFeedStatus
	// fetch overrides the default HTTP fetcher; nil means use httpFetchBlocklist.
	fetch blocklistFetcher
}

// begin marks an apply as running and seeds per-feed status. It returns false
// if an apply is already in progress.
func (m *blocklistManager) begin(feeds []BlocklistFeed) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return false
	}
	m.running = true
	if m.feeds == nil {
		m.feeds = make(map[string]*BlocklistFeedStatus)
	}
	now := time.Now()
	for _, f := range feeds {
		m.feeds[f.Key] = &BlocklistFeedStatus{Key: f.Key, Started: now, Updated: now}
	}
	return true
}

func (m *blocklistManager) finish() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
}

func (m *blocklistManager) update(key string, fn func(*BlocklistFeedStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.feeds[key]
	if !ok {
		return
	}
	fn(st)
	st.Updated = time.Now()
}

func (m *blocklistManager) snapshot() (bool, []BlocklistFeedStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]BlocklistFeedStatus, 0, len(m.feeds))
	for _, st := range m.feeds {
		out = append(out, *st)
	}
	return m.running, out
}

// httpFetchBlocklist is the default fetcher: a GET with a generous timeout and
// the system CA bundle (Go default). The caller closes the returned body.
func httpFetchBlocklist(ctx context.Context, feedURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "town-os-dns/blocklist")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// parseBlocklist reads a blocklist in hosts-file, plain-domain, or Adblock
// syntax and returns the de-duplicated set of blockable domains in file order.
func parseBlocklist(r io.Reader) ([]string, error) {
	seen := make(map[string]struct{})
	var out []string
	sc := bufio.NewScanner(r)
	// Allow long lines (some feeds pack many modifiers after a rule).
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		domain, ok := parseBlocklistLine(sc.Text())
		if !ok {
			continue
		}
		if _, dup := seen[domain]; dup {
			continue
		}
		seen[domain] = struct{}{}
		out = append(out, domain)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// parseBlocklistLine extracts a single blockable domain from one line. ok is
// false for comments, blank lines, and rules that do not map to a plain domain
// block (cosmetic filters, exceptions, regex, wildcards, raw IPs).
func parseBlocklistLine(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	switch line[0] {
	case '#', '!', '[': // hosts comment, Adblock comment, Adblock section header
		return "", false
	}

	// Adblock network rule: ||domain^ (optionally $modifiers). Anything else in
	// Adblock syntax (exceptions @@, cosmetic ##, regex /.../) is not a plain
	// domain block we can honor.
	if strings.HasPrefix(line, "||") {
		return parseAdblockDomain(line)
	}
	if strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "/") ||
		strings.Contains(line, "##") || strings.Contains(line, "#@#") {
		return "", false
	}

	// Strip a trailing inline comment (domains never contain '#').
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if line == "" {
		return "", false
	}

	fields := strings.Fields(line)
	var candidate string
	switch len(fields) {
	case 0:
		return "", false
	case 1:
		candidate = fields[0]
	default:
		// hosts format: "<sink-ip> domain [domain...]" — first field is the IP.
		if net.ParseIP(fields[0]) == nil {
			return "", false
		}
		candidate = fields[1]
	}
	return normalizeBlocklistDomain(candidate)
}

// parseAdblockDomain pulls the domain out of an "||domain^..." rule.
func parseAdblockDomain(line string) (string, bool) {
	rest := strings.TrimPrefix(line, "||")
	if end := strings.IndexAny(rest, "^/$"); end >= 0 {
		rest = rest[:end]
	}
	return normalizeBlocklistDomain(rest)
}

// normalizeBlocklistDomain lowercases and validates a candidate domain, rejecting
// wildcards, IPs, and anything that is not a fully-qualified hostname.
func normalizeBlocklistDomain(d string) (string, bool) {
	d = strings.ToLower(strings.TrimSpace(d))
	d = strings.TrimSuffix(d, ".")
	if d == "" {
		return "", false
	}
	if strings.ContainsAny(d, "*/?:@ \t|^") {
		return "", false
	}
	if d == "localhost" || net.ParseIP(d) != nil {
		return "", false
	}
	if validateHostname(d) != nil {
		return "", false
	}
	return d, true
}

// applyBlocklistFeed fetches, parses, and loads one feed into the local RBL
// list, returning the number of entries added. progress (may be nil) is called
// periodically with (added, total).
func applyBlocklistFeed(ctx context.Context, rc rolodex.Client, fetch blocklistFetcher, feed BlocklistFeed, progress func(added, total int)) (int, error) {
	body, err := fetch(ctx, feed.URL)
	if err != nil {
		return 0, fmt.Errorf("fetch %s: %w", feed.URL, err)
	}
	defer func() { _ = body.Close() }()

	domains, err := parseBlocklist(body)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", feed.Key, err)
	}

	reason := blocklistReasonPrefix + feed.Key
	added := 0
	for i, domain := range domains {
		if err := ctx.Err(); err != nil {
			return added, err
		}
		if err := rc.AddLocalRblEntry(ctx, &upstream.LocalRblEntry{Name: domain, Reason: reason}); err != nil {
			return added, fmt.Errorf("add %s: %w", domain, err)
		}
		added++
		if progress != nil && i%500 == 0 {
			progress(added, len(domains))
		}
	}
	if progress != nil {
		progress(added, len(domains))
	}
	return added, nil
}

// runBlocklistApply applies the given feeds sequentially in the background,
// updating per-feed status. It always clears the running flag on exit.
func (s *SystemControllerHandlers) runBlocklistApply(feeds []BlocklistFeed) {
	defer s.blocklist.finish()

	rc := s.Controller.GetRolodexClient()
	fetch := s.blocklist.fetch
	if fetch == nil {
		fetch = httpFetchBlocklist
	}

	for _, feed := range feeds {
		if rc == nil {
			s.blocklist.update(feed.Key, func(st *BlocklistFeedStatus) {
				st.Error = "rolodex not available"
				st.Done = true
			})
			continue
		}

		fctx, cancel := context.WithTimeout(s.ctx, blocklistApplyTimeout)
		added, err := applyBlocklistFeed(fctx, rc, fetch, feed, func(a, t int) {
			s.blocklist.update(feed.Key, func(st *BlocklistFeedStatus) {
				st.Added = a
				st.Total = t
			})
		})
		cancel()

		s.blocklist.update(feed.Key, func(st *BlocklistFeedStatus) {
			st.Added = added
			st.Done = true
			if err != nil {
				st.Error = err.Error()
			}
		})
	}
}

// --- HTTP handlers ---

// BlocklistsResponse is the response for GET /dns/blocklists.
type BlocklistsResponse struct {
	Feeds   []BlocklistFeed       `json:"feeds"`
	Running bool                  `json:"running"`
	Status  []BlocklistFeedStatus `json:"status"`
}

// ApplyBlocklistsRequest is the request body for POST /dns/blocklists/apply.
// Provide curated feed keys, or a custom feed via URL + Name. When both keys
// and URL are empty, all curated feeds are applied.
type ApplyBlocklistsRequest struct {
	Keys []string `json:"keys"`
	URL  string   `json:"url"`
	Name string   `json:"name"`
}

// ClearBlocklistsRequest is the request body for POST /dns/blocklists/clear.
// When Keys is empty, every blocklist-sourced local RBL entry is removed.
type ClearBlocklistsRequest struct {
	Keys []string `json:"keys"`
}

// listBlocklists handles GET /dns/blocklists.
func (s *SystemControllerHandlers) listBlocklists(c *echo.Context) error {
	running, status := s.blocklist.snapshot()
	return c.JSON(200, BlocklistsResponse{
		Feeds:   curatedBlocklists,
		Running: running,
		Status:  status,
	})
}

// resolveApplyFeeds turns a request into the concrete list of feeds to apply.
func resolveApplyFeeds(req ApplyBlocklistsRequest) ([]BlocklistFeed, error) {
	if req.URL != "" {
		u, err := url.Parse(req.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("invalid feed URL %q", req.URL)
		}
		key := strings.ToLower(strings.TrimSpace(req.Name))
		if key == "" {
			key = "custom"
		}
		if !dnsLabelPattern.MatchString(key) {
			return nil, fmt.Errorf("invalid feed name %q: must be alphanumeric with optional internal hyphens", req.Name)
		}
		return []BlocklistFeed{{Key: key, Name: req.Name, URL: req.URL}}, nil
	}

	keys := req.Keys
	if len(keys) == 0 {
		return append([]BlocklistFeed(nil), curatedBlocklists...), nil
	}
	feeds := make([]BlocklistFeed, 0, len(keys))
	for _, k := range keys {
		feed, ok := curatedBlocklistByKey(strings.ToLower(strings.TrimSpace(k)))
		if !ok {
			return nil, fmt.Errorf("unknown blocklist %q", k)
		}
		feeds = append(feeds, feed)
	}
	return feeds, nil
}

// applyBlocklists handles POST /dns/blocklists/apply. It starts a background
// apply and returns 202 immediately; progress is observable via GET
// /dns/blocklists.
func (s *SystemControllerHandlers) applyBlocklists(c *echo.Context) error {
	if s.Controller.GetRolodexClient() == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req ApplyBlocklistsRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	feeds, err := resolveApplyFeeds(req)
	if err != nil {
		return echo.NewHTTPError(400, err.Error())
	}

	if !s.blocklist.begin(feeds) {
		return echo.NewHTTPError(409, "a blocklist apply is already running")
	}

	go s.runBlocklistApply(feeds)

	keys := make([]string, 0, len(feeds))
	for _, f := range feeds {
		keys = append(keys, f.Key)
	}
	return c.JSON(202, map[string]any{"status": "started", "feeds": keys})
}

// clearBlocklists handles POST /dns/blocklists/clear. It removes local RBL
// entries that originated from blocklist feeds (reason prefixed "blocklist:").
func (s *SystemControllerHandlers) clearBlocklists(c *echo.Context) error {
	rc := s.Controller.GetRolodexClient()
	if rc == nil {
		return echo.NewHTTPError(503, "rolodex not available")
	}

	var req ClearBlocklistsRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	// Build the set of reasons to clear. Empty keys => all blocklist entries.
	wantReasons := make(map[string]struct{}, len(req.Keys))
	for _, k := range req.Keys {
		wantReasons[blocklistReasonPrefix+strings.ToLower(strings.TrimSpace(k))] = struct{}{}
	}

	ctx := c.Request().Context()
	entries, err := rc.ListLocalRblEntries(ctx)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list local rbl entries: %v", err))
	}

	removed := 0
	for _, e := range entries {
		if e == nil || !strings.HasPrefix(e.Reason, blocklistReasonPrefix) {
			continue
		}
		if len(wantReasons) > 0 {
			if _, ok := wantReasons[e.Reason]; !ok {
				continue
			}
		}
		if err := ctx.Err(); err != nil {
			return echo.NewHTTPError(500, fmt.Sprintf("clear interrupted: %v", err))
		}
		if err := rc.RemoveLocalRblEntry(ctx, e.Name); err != nil {
			return echo.NewHTTPError(500, fmt.Sprintf("remove %s: %v", e.Name, err))
		}
		removed++
	}

	return c.JSON(200, map[string]any{"status": "ok", "removed": removed})
}
