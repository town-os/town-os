// Package systemcontroller implements the Control Plane Service HTTP API
// and its Go client. The [Client] interface abstracts all API operations so
// callers can work against a live server or a [MockClient] in tests.
package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"bufio"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// Client defines the interface for communicating with the Control Plane Service.
// It covers storage, repository, package, systemd, account, audit, settings,
// upgrade, and archive operations. [SystemdClient] provides the HTTP
// implementation; [MockClient] is the in-memory test double.
type Client interface {
	// CreateFilesystem creates a new btrfs subvolume.
	CreateFilesystem(ctx context.Context, fs storage.Filesystem) error
	// ModifyFilesystem renames or resizes an existing filesystem.
	// Renaming is only allowed for user filesystems; package volumes
	// (installed/ or uninstalled/ prefix) cannot be renamed and will
	// return storage.ErrPackageVolumeRename if fs.Name differs from name.
	ModifyFilesystem(ctx context.Context, name string, fs storage.Filesystem) error
	// RemoveFilesystem deletes a filesystem by name.
	RemoveFilesystem(ctx context.Context, name string) error
	// ListFilesystems returns a paginated list of filesystems, optionally
	// filtered by name prefix and state. The state parameter accepts "user",
	// "installed", or "uninstalled"; an empty string returns all states.
	// The prefix parameter filters filesystems whose name starts with the
	// given string; an empty string matches all.
	ListFilesystems(ctx context.Context, prefix string, state string, params ListParams) (*PageResult[storage.Filesystem], error)

	// AddRepository registers a new package repository with optional credentials.
	AddRepository(ctx context.Context, name, rawURL, username, password string) error
	// RemoveRepository removes a package repository by name.
	RemoveRepository(ctx context.Context, name string) error
	// MoveRepository changes the priority position of a repository. Repositories
	// are checked in order during package resolution; position 0 is highest
	// priority.
	MoveRepository(ctx context.Context, name string, position int) error
	// RefreshRepositories triggers a refresh of all repository metadata.
	// Returns a map of repository names to error messages for any that failed.
	RefreshRepositories(ctx context.Context) (map[string]string, error)
	// ListRepositories returns a paginated list of configured repositories.
	ListRepositories(ctx context.Context, params ListParams) (*PageResult[RepositoryInfo], error)

	// ListFeaturedPackages returns featured packages grouped by repository,
	// including descriptions and install status. Returns only packages that
	// appear in a repository's featured.json file.
	// Calls GET /packages/featured on the Control Plane Service.
	ListFeaturedPackages(ctx context.Context) ([]FeaturedRepoGroup, error)
	// ListPackages returns a paginated list of available packages across all repos.
	ListPackages(ctx context.Context, params ListParams) (*PageResult[PackageListEntry], error)
	// ListPackagesByRepo returns packages grouped by their source repository.
	ListPackagesByRepo(ctx context.Context, params ListParams) ([]packages.RepoPackageGroup, error)
	// ListPackageVersions returns available versions of the named package.
	ListPackageVersions(ctx context.Context, name string) ([]string, error)
	// GetPackageQuestions returns the configuration questions for a package by name.
	GetPackageQuestions(ctx context.Context, name string) (map[string]packages.Question, error)
	// GetPackageQuestionsByIdentity returns configuration questions for a
	// specific package version identified by repo, name, and version.
	GetPackageQuestionsByIdentity(ctx context.Context, repo, name, version string) (map[string]packages.Question, error)

	// ListChildren returns the child package names for a given repo and package.
	ListChildren(ctx context.Context, repo, name string) ([]string, error)
	// InstallPreview returns volume and port information that would be created
	// if the package were installed, without performing the installation.
	InstallPreview(ctx context.Context, repo, name, version string) (*InstallPreview, error)
	// InstallPackage installs a package at the given version with the provided
	// configuration responses (a map of question keys to answer strings).
	//
	// The name parameter uses the format "repo/package" (e.g. "myrepo/nginx").
	// When reuseVolumes is true, existing data volumes from a prior installation
	// are preserved. When importFromVersion is non-empty, data is imported from
	// the specified prior version. When skipResponseReuse is true, previously
	// stored configuration responses are not merged into the provided responses.
	InstallPackage(ctx context.Context, name, version string, responses packages.Responses, reuseVolumes bool, importFromVersion string, skipResponseReuse bool) error
	// UninstallPackage removes an installed package. Set purgeVolumes to also
	// delete all associated data volumes.
	UninstallPackage(ctx context.Context, repo, name, version string, purgeVolumes bool) error
	// PurgeVolumes deletes all data volumes for the named package.
	PurgeVolumes(ctx context.Context, repo, name string) error
	// ListUninstalledVolumes returns information about volumes left behind by
	// previously uninstalled packages.
	ListUninstalledVolumes(ctx context.Context, repo, name string) (*UninstalledVolumesResponse, error)
	// PurgeUninstalledVolumes deletes leftover volumes from uninstalled packages.
	PurgeUninstalledVolumes(ctx context.Context, repo, name string) error
	// DisablePackage stops a package's services without uninstalling it.
	DisablePackage(ctx context.Context, repo, name string) error
	// EnablePackage re-enables a previously disabled package.
	EnablePackage(ctx context.Context, repo, name string) error
	// ListInstalled returns a paginated list of installed package identifiers
	// in the format "repo/name@version" (e.g. "myrepo/nginx@1.0.0").
	ListInstalled(ctx context.Context, params ListParams) (*PageResult[string], error)
	// GetResponses returns the stored configuration responses for an installed package.
	GetResponses(ctx context.Context, repo, name, version string) (packages.Responses, error)
	// GetLastResponses returns the most recently stored configuration responses
	// for a package (across all versions), useful for pre-filling install forms.
	GetLastResponses(ctx context.Context, repo, name string) (packages.Responses, error)
	// ClearLastResponses removes the cached last-responses for a package.
	ClearLastResponses(ctx context.Context, repo, name string) error
	// GetInstalledInfo returns detailed information about an installed package,
	// including its questions, responses, and notes.
	GetInstalledInfo(ctx context.Context, repo, name, version string) (*InstalledInfoResponse, error)

	// ListUnits returns a paginated list of systemd service units.
	ListUnits(ctx context.Context, params ListParams) (*PageResult[UnitListEntry], error)
	// SetUnitStatus applies a [systemd.StatusAction] to a systemd unit. Valid
	// actions are "start", "stop", and "restart".
	SetUnitStatus(ctx context.Context, name string, action systemd.StatusAction) error
	// LogReplay streams historical journal entries for a unit as server-sent events.
	// The returned channel is closed when the replay is complete.
	LogReplay(ctx context.Context, name string) (<-chan systemd.JournalEntry, error)
	// LogTail returns a page of recent journal entries for a unit with
	// cursor-based pagination. See [systemd.LogTailParams] for available
	// filter and pagination options including grep, time range, and priority.
	LogTail(ctx context.Context, params systemd.LogTailParams) (systemd.LogTailResult, error)
	// LogReplayTree streams historical journal entries for every systemd
	// unit in a package's dependency tree as a single SSE stream. The
	// (repo, name, version) triple identifies the root package; dependency
	// units are expanded server-side from the installed records.
	LogReplayTree(ctx context.Context, repo, name, version string) (<-chan systemd.JournalEntry, error)
	// LogTailTree returns a page of recent journal entries covering every
	// systemd unit in a package's dependency tree. Filters and cursors
	// carried on [systemd.LogTailParams] behave identically to LogTail;
	// the Unit and Units fields are ignored (the unit set is derived from
	// the install records for the named package).
	LogTailTree(ctx context.Context, repo, name, version string, params systemd.LogTailParams) (systemd.LogTailResult, error)

	// CreateAccount creates a new user account. The password must be at least
	// 8 characters. Email, phone, and realName are required fields. When admin
	// is true the account receives administrator privileges.
	CreateAccount(ctx context.Context, username, password, email, phone, realName string, admin bool) (*account.Account, error)
	// GetAccount retrieves a user account by username.
	GetAccount(ctx context.Context, username string) (*account.Account, error)
	// UpdateAccount modifies fields on an existing user account. Only non-nil
	// pointer fields in [account.UpdateFields] are applied (password, email,
	// phone, real_name, admin).
	UpdateAccount(ctx context.Context, username string, fields account.UpdateFields) (*account.Account, error)
	// DisableAccount prevents a user from authenticating.
	DisableAccount(ctx context.Context, username string) error
	// EnableAccount re-enables a disabled user account.
	EnableAccount(ctx context.Context, username string) error
	// ListAccounts returns a paginated list of all user accounts.
	ListAccounts(ctx context.Context, params ListParams) (*PageResult[account.Account], error)
	// Authenticate validates credentials and returns a session token.
	Authenticate(ctx context.Context, username, password string) (*AuthenticateResponse, error)
	// RevokeSession invalidates a session by its ID.
	RevokeSession(ctx context.Context, sessionID string) error
	// ListSessions returns all active sessions for the given token's user.
	ListSessions(ctx context.Context, token string) ([]account.Session, error)
	// SessionUsername returns the username associated with the given session token.
	SessionUsername(ctx context.Context, token string) (string, error)

	// ListAuditLog returns a paginated audit log filtered by the given options.
	ListAuditLog(ctx context.Context, opts account.AuditListOptions, token string) (*account.AuditPage, error)

	// GetSettings returns all system settings as a key-value map.
	GetSettings(ctx context.Context) (map[string]string, error)
	// GetSetting returns the value of a single setting by key.
	GetSetting(ctx context.Context, key string) (string, error)
	// SetSetting updates a single system setting.
	SetSetting(ctx context.Context, key, value string) error

	// ListUpgrades returns packages that have newer versions available.
	ListUpgrades(ctx context.Context) ([]PackageUpgrade, error)
	// DismissUpgrades marks all pending upgrades as dismissed.
	DismissUpgrades(ctx context.Context) error

	// UploadArchive uploads and extracts an archive into the named subvolume.
	// Supported formats are tar.gz, tar.bz2, and tar.xz (detected from the
	// filename extension). When subpath is non-empty, extraction is limited
	// to that directory within the subvolume. The stopService parameter,
	// when non-empty, names a systemd service to stop before extraction and
	// restart afterward.
	UploadArchive(ctx context.Context, subvolume string, archiveReader io.Reader, filename, subpath, stopService string) (*ArchiveUploadResponse, error)
	// DownloadArchive creates an archive of the specified paths within a
	// subvolume and returns a reader for the archive data. The format parameter
	// selects the compression: "tar.gz" (default), "tar.bz2", or "tar.xz".
	// The filename parameter sets the download filename in the server's
	// Content-Disposition header; when empty, the server uses "download"
	// with the appropriate archive extension. The caller must close the
	// returned reader.
	//
	// Calls POST /storage/download-archive on the Control Plane Service.
	DownloadArchive(ctx context.Context, subvolume string, paths []string, stopService, format, filename string) (io.ReadCloser, error)

	// RebuildGitSources pulls and updates git sources for an installed package.
	RebuildGitSources(ctx context.Context, repo, name, version string) error

	// CreatePage creates a new page site for static hosting.
	// sourceType selects the content source: "archive" (default), "container_image", or "git".
	// repoURL and branch are required when sourceType is "git".
	// image and imageDirectory are required when sourceType is "container_image".
	CreatePage(ctx context.Context, name, repoURL, branch, domain, sourceType, image, imageDirectory, network string) (*account.PageSite, error)
	// UpdatePage updates fields on an existing page site.
	UpdatePage(ctx context.Context, name string, fields account.PageSiteUpdate) (*account.PageSite, error)
	// RemovePage deletes a page site.
	RemovePage(ctx context.Context, name string) error
	// ListPages returns a paginated list of page sites.
	ListPages(ctx context.Context, params ListParams) (*PageResult[account.PageSite], error)
	// RebuildPage refreshes page content. For git pages it pulls the latest
	// changes; for container_image pages it re-extracts from the image;
	// archive pages must be rebuilt via UploadPageArchive instead.
	RebuildPage(ctx context.Context, name string) (*account.PageSite, error)
	// UploadPageArchive uploads and extracts an archive for an archive-type page.
	// The archive data is read from archiveReader and the filename is used for
	// format detection.
	UploadPageArchive(ctx context.Context, name string, archiveReader io.Reader, filename string) (*account.PageSite, error)

	// ListLocales returns available locales, the current system locale,
	// common languages with native names, and extended locale codes.
	// Calls GET /locales on the Control Plane Service.
	ListLocales(ctx context.Context) (*LocaleListResponse, error)

	// Ping returns service health and summary counts.
	Ping(ctx context.Context) (*PingResponse, error)

	// MonitoringStatus returns the current state of the monitoring stack.
	//
	// Calls GET /monitoring/status on the Control Plane Service.
	MonitoringStatus(ctx context.Context) (*monitoring.MonitoringStatus, error)

	// ListSystemServices returns system services with their current status.
	//
	// Calls GET /system-services on the Control Plane Service.
	ListSystemServices(ctx context.Context) ([]SystemServiceEntry, error)
	// SetSystemServiceStatus applies an action (start, stop, restart) to a
	// system service identified by key. Enable and disable are rejected.
	//
	// Calls POST /system-services/status on the Control Plane Service.
	SetSystemServiceStatus(ctx context.Context, key string, action systemd.StatusAction) error

	// DNSStatus returns the current state of the DNS service including
	// whether rolodex is enabled/running, the current TLD, and record count.
	//
	// Calls GET /dns/status on the Control Plane Service.
	DNSStatus(ctx context.Context) (*DNSStatusResponse, error)
	// ListDNSRecords returns DNS records annotated with their network and TLD.
	// An empty tld returns records across every network (global + scoped); a
	// non-empty tld restricts the result to that domain.
	//
	// Calls GET /dns/records on the Control Plane Service.
	ListDNSRecords(ctx context.Context, tld string) ([]*DNSRecordView, error)
	// AddDNSRecord adds a custom DNS record via the rolodex service.
	//
	// Calls POST /dns/records/add on the Control Plane Service.
	AddDNSRecord(ctx context.Context, record *upstream.DnsRecord) error
	// RemoveDNSRecord removes DNS records matching the given name and
	// optional record type. Returns the number of records removed.
	//
	// Calls POST /dns/records/remove on the Control Plane Service.
	RemoveDNSRecord(ctx context.Context, name string, recordType *upstream.RecordType) (uint32, error)
	// GetDNSTLD returns the current TLD setting for DNS records.
	//
	// Calls GET /dns/tld on the Control Plane Service.
	GetDNSTLD(ctx context.Context) (string, error)
	// SetDNSTLD changes the TLD and re-provisions all DNS records under
	// the new TLD.
	//
	// Calls POST /dns/tld on the Control Plane Service.
	SetDNSTLD(ctx context.Context, tld string) error
	// SetupDNS initializes or reconciles the TLD zone and registers DNS
	// records for all installed packages.
	//
	// Calls POST /dns/setup on the Control Plane Service.
	SetupDNS(ctx context.Context) error

	// GetRblConfig returns the current RBL (reverse-IP blocklist) config.
	//
	// Calls GET /dns/rbl on the Control Plane Service.
	GetRblConfig(ctx context.Context) (*RblConfigResponse, error)
	// SetRblConfig replaces the RBL configuration.
	//
	// Calls POST /dns/rbl on the Control Plane Service.
	SetRblConfig(ctx context.Context, enabled bool, providers []RblProviderDTO) error
	// GetDnsblConfig returns the current DNSBL (domain blocklist) config.
	//
	// Calls GET /dns/dnsbl on the Control Plane Service.
	GetDnsblConfig(ctx context.Context) (*RblConfigResponse, error)
	// SetDnsblConfig replaces the DNSBL configuration.
	//
	// Calls POST /dns/dnsbl on the Control Plane Service.
	SetDnsblConfig(ctx context.Context, enabled bool, providers []RblProviderDTO) error
	// ListLocalRblEntries returns the local RBL blocklist entries.
	//
	// Calls GET /dns/rbl/local on the Control Plane Service.
	ListLocalRblEntries(ctx context.Context) ([]LocalRblEntryDTO, error)
	// AddLocalRblEntry adds a name or IP to the local RBL blocklist.
	//
	// Calls POST /dns/rbl/local/add on the Control Plane Service.
	AddLocalRblEntry(ctx context.Context, name, reason string) error
	// RemoveLocalRblEntry removes a name or IP from the local RBL blocklist.
	//
	// Calls POST /dns/rbl/local/remove on the Control Plane Service.
	RemoveLocalRblEntry(ctx context.Context, name string) error
	// ListDnsblAllowlistEntries returns the DNSBL allowlist entries — names
	// exempted from the name-based blocklist check.
	//
	// Calls GET /dns/dnsbl/allowlist on the Control Plane Service.
	ListDnsblAllowlistEntries(ctx context.Context) ([]DnsblAllowlistEntryDTO, error)
	// AddDnsblAllowlistEntry exempts a name (and every name beneath it) from
	// the name-based blocklist check.
	//
	// Calls POST /dns/dnsbl/allowlist/add on the Control Plane Service.
	AddDnsblAllowlistEntry(ctx context.Context, name, reason string) error
	// RemoveDnsblAllowlistEntry removes a name from the DNSBL allowlist.
	//
	// Calls POST /dns/dnsbl/allowlist/remove on the Control Plane Service.
	RemoveDnsblAllowlistEntry(ctx context.Context, name string) error
	// ListDNSServices returns installed package services with their published
	// (in-DNS-zone) state.
	//
	// Calls GET /dns/services on the Control Plane Service.
	ListDNSServices(ctx context.Context) ([]DNSServiceEntry, error)
	// SetDNSService publishes or unpublishes a package service in the DNS zone.
	//
	// Calls POST /dns/services/set on the Control Plane Service.
	SetDNSService(ctx context.Context, repo, name string, published bool) error

	// ListVMImages returns all cached VM disk images in the vm-images
	// subvolume. Each entry includes the image filename and size in bytes.
	//
	// Calls GET /vm-images on the Control Plane Service.
	ListVMImages(ctx context.Context) ([]VMImageInfo, error)
	// UploadVMImage downloads a VM disk image from a remote URL, converts
	// it to raw format using qemu-img, and caches the result in the
	// vm-images subvolume.
	//
	// Parameters:
	//   - url: remote URL to download the VM image from (required).
	//   - name: desired filename for the cached image. When empty, the
	//     filename is derived from the URL's last path segment.
	//
	// Calls POST /vm-images/upload on the Control Plane Service.
	UploadVMImage(ctx context.Context, url, name string) (*VMImageInfo, error)
	// DeleteVMImage removes a cached VM disk image from the vm-images
	// subvolume.
	//
	// Parameters:
	//   - name: filename of the VM image to delete (required).
	//
	// Calls POST /vm-images/delete on the Control Plane Service.
	DeleteVMImage(ctx context.Context, name string) error
}

// Sentinel errors returned by [SystemdClient] methods.
var (
	ErrNewRequest         = errors.New("new request")
	ErrHTTPRequest        = errors.New("http request")
	ErrUnsuccessfulStatus = errors.New("unsuccessful status code")
)

// SystemdClient is the HTTP implementation of [Client]. It communicates with
// the Control Plane Service over a Unix socket or TCP connection, using
// JSON-encoded request bodies and bearer-token authentication.
type SystemdClient struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
}

// InitClient creates a [SystemdClient] that connects to the Control Plane
// Service via the given Unix domain socket path.
func InitClient(sock string) (*SystemdClient, error) {
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}},
		Timeout: time.Minute,
	}

	return FromClient(client, "http://localhost")
}

// FromClient wraps an existing [http.Client] and base URL into a
// [SystemdClient]. This is useful for testing or when the transport
// is already configured (e.g. TLS or custom dialer).
func FromClient(client *http.Client, baseURL string) (*SystemdClient, error) {
	return &SystemdClient{HTTP: client, BaseURL: baseURL}, nil
}

// route joins the base URL with the given API path, preserving any query string.
func (c *SystemdClient) route(path string) string {
	pathPart, query, hasQuery := strings.Cut(path, "?")
	result, err := url.JoinPath(c.BaseURL, pathPart)
	if err != nil {
		result = fmt.Sprintf("%s/%s", c.BaseURL, pathPart)
	}
	if hasQuery {
		result = fmt.Sprintf("%s?%s", result, query)
	}
	return result
}

// pipeEncode JSON-encodes v into the pipe writer and closes it.
func pipeEncode(pw *io.PipeWriter, v any) {
	pw.CloseWithError(json.NewEncoder(pw).Encode(v))
}

// postClient sends a POST request with a JSON body and expects a 200 response
// with no decoded body. It returns a [ProblemError] on non-200 responses.
func (c *SystemdClient) postClient(ctx context.Context, path string, body io.Reader) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route(path), body)
	if err != nil {
		return fmt.Errorf("%w: POST %s: %w", ErrNewRequest, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req) //nolint:gosec // G704 -- URL from trusted c.URL
	if err != nil {
		return fmt.Errorf("%w: POST %s: %w", ErrHTTPRequest, path, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return readProblemDetail(resp, "POST", path)
	}

	return nil
}

// postSSE sends a POST request and reads the SSE progress stream until a
// "done" or "error" event is received. Returns nil on success or the error
// message from the stream. Validation errors (non-200 status) are returned
// as ProblemDetail errors before reading the stream.
func (c *SystemdClient) postSSE(ctx context.Context, path string, body io.Reader) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route(path), body)
	if err != nil {
		return fmt.Errorf("%w: POST %s: %w", ErrNewRequest, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req) //nolint:gosec // G704 -- URL from trusted c.URL
	if err != nil {
		return fmt.Errorf("%w: POST %s: %w", ErrHTTPRequest, path, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	// Non-200 means validation failed before SSE started.
	if resp.StatusCode != http.StatusOK {
		return readProblemDetail(resp, "POST", path)
	}

	// Read SSE stream for done/error events.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var evt progressEvent
		if err := json.Unmarshal([]byte(line[6:]), &evt); err != nil {
			continue
		}
		if evt.Error != "" {
			return fmt.Errorf("POST %s: %s", path, evt.Error)
		}
		if evt.Done {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("POST %s: read SSE: %w", path, err)
	}

	return nil
}

// getClient sends a GET request and returns the raw response. The caller is
// responsible for closing the response body and checking the status code.
func (c *SystemdClient) getClient(ctx context.Context, path string) (_ *http.Response, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.route(path), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: GET %s: %w", ErrNewRequest, path, err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return c.HTTP.Do(req) //nolint:gosec // G704 -- URL from trusted c.URL
}

// postJSON sends a POST request with a JSON body and returns the raw response.
// The caller is responsible for closing the body and decoding the result.
func (c *SystemdClient) postJSON(ctx context.Context, path string, body io.Reader) (_ *http.Response, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route(path), body)
	if err != nil {
		return nil, fmt.Errorf("%w: POST %s: %w", ErrNewRequest, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return c.HTTP.Do(req) //nolint:gosec // G704 -- URL from trusted c.URL
}
