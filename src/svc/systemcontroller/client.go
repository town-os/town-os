// Package systemcontroller implements the Control Plane Service HTTP API
// and its Go client. The [Client] interface abstracts all API operations so
// callers can work against a live server or a [MockClient] in tests.
package systemcontroller

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
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

	// ListTimezones returns the list of available IANA timezone names.
	ListTimezones(ctx context.Context) ([]string, error)
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
	// selects the compression: "tar.gz", "tar.bz2", or "tar.xz". The caller
	// must close the returned reader.
	DownloadArchive(ctx context.Context, subvolume string, paths []string, stopService, format string) (io.ReadCloser, error)

	// Ping returns service health and summary counts.
	Ping(ctx context.Context) (*PingResponse, error)
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
		Timeout: 60 * time.Second,
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
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token)) //nolint:perfsprint // project convention: use fmt.Sprintf
	}

	resp, err := c.HTTP.Do(req)
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

// getClient sends a GET request and returns the raw response. The caller is
// responsible for closing the response body and checking the status code.
func (c *SystemdClient) getClient(ctx context.Context, path string) (_ *http.Response, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.route(path), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: GET %s: %w", ErrNewRequest, path, err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token)) //nolint:perfsprint // project convention: use fmt.Sprintf
	}
	return c.HTTP.Do(req)
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
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token)) //nolint:perfsprint // project convention: use fmt.Sprintf
	}
	return c.HTTP.Do(req)
}

// --- Storage ---

// CreateFilesystem creates a new btrfs subvolume.
func (c *SystemdClient) CreateFilesystem(ctx context.Context, fs storage.Filesystem) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, fs)

	return c.postClient(ctx, "storage/create", pr)
}

// ModifyFilesystem renames or resizes an existing filesystem.
func (c *SystemdClient) ModifyFilesystem(ctx context.Context, name string, fs storage.Filesystem) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, ModifyFilesystemRequest{Name: name, Filesystem: fs})

	return c.postClient(ctx, "storage/modify", pr)
}

// RemoveFilesystem deletes a filesystem by name.
func (c *SystemdClient) RemoveFilesystem(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, FilesystemName{Name: name})

	return c.postClient(ctx, "storage/remove", pr)
}

// ListFilesystems returns a paginated list of filesystems, optionally filtered
// by name prefix and state.
func (c *SystemdClient) ListFilesystems(ctx context.Context, prefix string, state string, params ListParams) (_ *PageResult[storage.Filesystem], err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, FilesystemName{
		Name:      prefix,
		State:     state,
		SortBy:    params.SortBy,
		SortOrder: params.SortOrder,
		Limit:     params.Limit,
		Offset:    params.Offset,
		Search:    params.Search,
	})

	resp, err := c.postJSON(ctx, "storage", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListFilesystems: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "storage")
	}

	var page PageResult[storage.Filesystem]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// --- Repository ---

// AddRepository registers a new package repository with optional credentials.
func (c *SystemdClient) AddRepository(ctx context.Context, name, rawURL, username, password string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AddRepositoryRequest{Name: name, URL: rawURL, Username: username, Password: password})

	return c.postClient(ctx, "repository/add", pr)
}

// RemoveRepository removes a package repository by name.
func (c *SystemdClient) RemoveRepository(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RepositoryNameRequest{Name: name})

	return c.postClient(ctx, "repository/remove", pr)
}

// MoveRepository changes the priority position of a repository.
func (c *SystemdClient) MoveRepository(ctx context.Context, name string, position int) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, MoveRepositoryRequest{Name: name, Position: position})

	return c.postClient(ctx, "repository/move", pr)
}

// RefreshRepositories triggers a refresh of all repository metadata.
// Returns a map of repository names to error messages for any that failed.
func (c *SystemdClient) RefreshRepositories(ctx context.Context) (_ map[string]string, err error) {
	resp, err := c.postJSON(ctx, "repository/refresh", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: RefreshRepositories: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "repository/refresh")
	}

	var errs map[string]string
	err = json.NewDecoder(resp.Body).Decode(&errs)
	if err != nil {
		return nil, nil //nolint:nilnil // intentional: nil list with no error means "not found"
	}
	return errs, nil
}

// ListRepositories returns a paginated list of configured repositories.
func (c *SystemdClient) ListRepositories(ctx context.Context, params ListParams) (_ *PageResult[RepositoryInfo], err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("repository%s", params.QueryString())) //nolint:perfsprint // project convention: use fmt.Sprintf
	if err != nil {
		return nil, fmt.Errorf("%w: ListRepositories: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "repository")
	}

	var page PageResult[RepositoryInfo]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// --- Packages ---

// ListTimezones returns the list of available IANA timezone names.
func (c *SystemdClient) ListTimezones(ctx context.Context) (_ []string, err error) {
	resp, err := c.getClient(ctx, "packages/timezones")
	if err != nil {
		return nil, fmt.Errorf("%w: ListTimezones: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "packages/timezones")
	}

	var zones []string
	return zones, json.NewDecoder(resp.Body).Decode(&zones)
}

// ListPackages returns a paginated list of available packages across all repos.
func (c *SystemdClient) ListPackages(ctx context.Context, params ListParams) (_ *PageResult[PackageListEntry], err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("packages%s", params.QueryString())) //nolint:perfsprint // project convention: use fmt.Sprintf
	if err != nil {
		return nil, fmt.Errorf("%w: ListPackages: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "packages")
	}

	var page PageResult[PackageListEntry]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// ListPackagesByRepo returns packages grouped by their source repository.
func (c *SystemdClient) ListPackagesByRepo(ctx context.Context, params ListParams) (_ []packages.RepoPackageGroup, err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("packages/by-repo%s", params.QueryString())) //nolint:perfsprint // project convention: use fmt.Sprintf
	if err != nil {
		return nil, fmt.Errorf("%w: ListPackagesByRepo: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "packages/by-repo")
	}

	var groups []packages.RepoPackageGroup
	return groups, json.NewDecoder(resp.Body).Decode(&groups)
}

// ListPackageVersions returns available versions of the named package.
func (c *SystemdClient) ListPackageVersions(ctx context.Context, name string) (_ []string, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Name: name})

	resp, err := c.postJSON(ctx, "packages/versions", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListPackageVersions: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/versions")
	}

	var versions []string
	return versions, json.NewDecoder(resp.Body).Decode(&versions)
}

// GetPackageQuestions returns the configuration questions for a package by name.
func (c *SystemdClient) GetPackageQuestions(ctx context.Context, name string) (_ map[string]packages.Question, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Name: name})

	resp, err := c.postJSON(ctx, "packages/questions", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetPackageQuestions: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/questions")
	}

	var questions map[string]packages.Question
	return questions, json.NewDecoder(resp.Body).Decode(&questions)
}

// GetPackageQuestionsByIdentity returns configuration questions for a specific
// package version identified by repo, name, and version.
func (c *SystemdClient) GetPackageQuestionsByIdentity(ctx context.Context, repo, name, version string) (_ map[string]packages.Question, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageIdentityRequest{Repo: repo, Name: name, Version: version})

	resp, err := c.postJSON(ctx, "packages/questions/identity", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetPackageQuestionsByIdentity: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/questions/identity")
	}

	var questions map[string]packages.Question
	return questions, json.NewDecoder(resp.Body).Decode(&questions)
}

// --- Install ---

// InstallPackage installs a package at the given version with the provided
// configuration responses.
func (c *SystemdClient) InstallPackage(ctx context.Context, name, version string, responses packages.Responses, reuseVolumes bool, importFromVersion string, skipResponseReuse bool) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, InstallRequest{
		Name:              name,
		Version:           version,
		Responses:         responses,
		ReuseVolumes:      reuseVolumes,
		ImportFromVersion: importFromVersion,
		SkipResponseReuse: skipResponseReuse,
	})

	return c.postClient(ctx, "packages/install", pr)
}

// UninstallPackage removes an installed package. Set purgeVolumes to also
// delete all associated data volumes.
func (c *SystemdClient) UninstallPackage(ctx context.Context, repo, name, version string, purgeVolumes bool) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, UninstallRequest{Repo: repo, Name: name, Version: version, PurgeVolumes: purgeVolumes})

	return c.postClient(ctx, "packages/uninstall", pr)
}

// PurgeVolumes deletes all data volumes for the named package.
func (c *SystemdClient) PurgeVolumes(ctx context.Context, repo, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Repo: repo, Name: name})

	return c.postClient(ctx, "packages/purge-volumes", pr)
}

// ListUninstalledVolumes returns information about volumes left behind by
// previously uninstalled packages.
func (c *SystemdClient) ListUninstalledVolumes(ctx context.Context, repo, name string) (_ *UninstalledVolumesResponse, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Repo: repo, Name: name})

	resp, err := c.postJSON(ctx, "packages/uninstalled-volumes", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListUninstalledVolumes: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/uninstalled-volumes")
	}

	var result UninstalledVolumesResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}

// PurgeUninstalledVolumes deletes leftover volumes from uninstalled packages.
func (c *SystemdClient) PurgeUninstalledVolumes(ctx context.Context, repo, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Repo: repo, Name: name})

	return c.postClient(ctx, "packages/purge-uninstalled-volumes", pr)
}

// DisablePackage stops a package's services without uninstalling it.
func (c *SystemdClient) DisablePackage(ctx context.Context, repo, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageToggleRequest{Repo: repo, Name: name})

	return c.postClient(ctx, "packages/disable", pr)
}

// EnablePackage re-enables a previously disabled package.
func (c *SystemdClient) EnablePackage(ctx context.Context, repo, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageToggleRequest{Repo: repo, Name: name})

	return c.postClient(ctx, "packages/enable", pr)
}

// ListInstalled returns a paginated list of installed package identifiers.
func (c *SystemdClient) ListInstalled(ctx context.Context, params ListParams) (_ *PageResult[string], err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("packages/installed%s", params.QueryString())) //nolint:perfsprint // project convention: use fmt.Sprintf
	if err != nil {
		return nil, fmt.Errorf("%w: ListInstalled: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "packages/installed")
	}

	var page PageResult[string]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// GetResponses returns the stored configuration responses for an installed package.
func (c *SystemdClient) GetResponses(ctx context.Context, repo, name, version string) (_ packages.Responses, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, GetResponsesRequest{Repo: repo, Name: name, Version: version})

	resp, err := c.postJSON(ctx, "packages/responses", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetResponses: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/responses")
	}

	var responses packages.Responses
	return responses, json.NewDecoder(resp.Body).Decode(&responses)
}

// GetInstalledInfo returns detailed information about an installed package.
func (c *SystemdClient) GetInstalledInfo(ctx context.Context, repo, name, version string) (_ *InstalledInfoResponse, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageIdentityRequest{Repo: repo, Name: name, Version: version})

	resp, err := c.postJSON(ctx, "packages/installed/info", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetInstalledInfo: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/installed/info")
	}

	var info InstalledInfoResponse
	return &info, json.NewDecoder(resp.Body).Decode(&info)
}

// ListChildren returns the child package names for a given repo and package.
func (c *SystemdClient) ListChildren(ctx context.Context, repo, name string) (_ []string, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, ListChildrenRequest{Repo: repo, Name: name})

	resp, err := c.postJSON(ctx, "packages/children", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListChildren: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/children")
	}

	var children []string
	return children, json.NewDecoder(resp.Body).Decode(&children)
}

// InstallPreview returns volume and port information that would be created
// if the package were installed, without performing the installation.
func (c *SystemdClient) InstallPreview(ctx context.Context, repo, name, version string) (_ *InstallPreview, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, InstallPreviewRequest{Repo: repo, Name: name, Version: version})

	resp, err := c.postJSON(ctx, "packages/install-preview", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: InstallPreview: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "packages/install-preview")
	}

	var preview InstallPreview
	return &preview, json.NewDecoder(resp.Body).Decode(&preview)
}

// --- Systemd ---

// ListUnits returns a paginated list of systemd service units.
func (c *SystemdClient) ListUnits(ctx context.Context, params ListParams) (_ *PageResult[UnitListEntry], err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("systemd/units%s", params.QueryString())) //nolint:perfsprint // project convention: use fmt.Sprintf
	if err != nil {
		return nil, fmt.Errorf("%w: ListUnits: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "systemd/units")
	}

	var page PageResult[UnitListEntry]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// SetUnitStatus applies a status action (start, stop, restart) to a systemd unit.
func (c *SystemdClient) SetUnitStatus(ctx context.Context, name string, action systemd.StatusAction) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, SetStatusRequest{Name: name, Action: action})

	return c.postClient(ctx, "systemd/status", pr)
}

// LogReplay streams historical journal entries for a unit via server-sent
// events. The returned channel is closed when the replay completes.
func (c *SystemdClient) LogReplay(ctx context.Context, name string) (_ <-chan systemd.JournalEntry, err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("systemd/logs?unit=%s", url.QueryEscape(name))) //nolint:bodyclose,perfsprint // closed in goroutine defer below; project convention: use fmt.Sprintf
	if err != nil {
		if resp != nil {
			err = errors.Join(err, resp.Body.Close())
		}
		return nil, fmt.Errorf("%w: LogReplay: %w", ErrHTTPRequest, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetailAndClose(resp, "GET", "systemd/logs")
	}

	ch := make(chan systemd.JournalEntry)
	go func() {
		defer close(ch)
		defer func() {
			err = errors.Join(err, resp.Body.Close())
		}()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			var entry systemd.JournalEntry
			err := json.NewDecoder(strings.NewReader(strings.TrimPrefix(line, "data: "))).Decode(&entry)
			if err != nil {
				return
			}
			ch <- entry
		}
	}()

	return ch, nil
}

// LogTail returns a page of recent journal entries with cursor-based pagination.
func (c *SystemdClient) LogTail(ctx context.Context, p systemd.LogTailParams) (_ systemd.LogTailResult, err error) {
	q := fmt.Sprintf("systemd/logs/tail?unit=%s&lines=%d", url.QueryEscape(p.Unit), p.Lines)
	if p.BeforeCursor != "" {
		q = fmt.Sprintf("%s&before=%s", q, url.QueryEscape(p.BeforeCursor))
	}
	if p.AfterCursor != "" {
		q = fmt.Sprintf("%s&after=%s", q, url.QueryEscape(p.AfterCursor))
	}
	if p.Grep != "" {
		q = fmt.Sprintf("%s&grep=%s", q, url.QueryEscape(p.Grep))
	}
	if !p.Since.IsZero() {
		q = fmt.Sprintf("%s&since=%d", q, p.Since.Unix())
	}
	if !p.Until.IsZero() {
		q = fmt.Sprintf("%s&until=%d", q, p.Until.Unix())
	}
	if p.Priority > 0 {
		q = fmt.Sprintf("%s&priority=%d", q, p.Priority)
	}

	resp, err := c.getClient(ctx, q)
	if err != nil {
		return systemd.LogTailResult{}, fmt.Errorf("%w: LogTail: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return systemd.LogTailResult{}, readProblemDetail(resp, "GET", "systemd/logs/tail")
	}

	var result systemd.LogTailResult
	return result, json.NewDecoder(resp.Body).Decode(&result)
}

// --- Account ---

// CreateAccount creates a new user account with the given profile fields.
// When admin is true the account receives administrator privileges.
func (c *SystemdClient) CreateAccount(ctx context.Context, username, password, email, phone, realName string, admin bool) (_ *account.Account, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, CreateAccountRequest{
		Username: username, Password: password,
		Email: email, Phone: phone, RealName: realName, Admin: admin,
	})

	resp, err := c.postJSON(ctx, "account/create", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: CreateAccount: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "account/create")
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

// GetAccount retrieves a user account by username.
func (c *SystemdClient) GetAccount(ctx context.Context, username string) (_ *account.Account, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, GetAccountRequest{Username: username})

	resp, err := c.postJSON(ctx, "account", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetAccount: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "account")
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

// UpdateAccount modifies fields on an existing user account. Only non-nil
// fields in the [account.UpdateFields] struct are applied.
func (c *SystemdClient) UpdateAccount(ctx context.Context, username string, fields account.UpdateFields) (_ *account.Account, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, UpdateAccountRequest{Username: username, Fields: fields})

	resp, err := c.postJSON(ctx, "account/update", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: UpdateAccount: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "account/update")
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

// DisableAccount prevents the named user from authenticating.
func (c *SystemdClient) DisableAccount(ctx context.Context, username string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, DisableAccountRequest{Username: username})

	return c.postClient(ctx, "account/disable", pr)
}

// EnableAccount re-enables a previously disabled user account.
func (c *SystemdClient) EnableAccount(ctx context.Context, username string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, EnableAccountRequest{Username: username})

	return c.postClient(ctx, "account/enable", pr)
}

// ListAccounts returns a paginated list of all user accounts.
func (c *SystemdClient) ListAccounts(ctx context.Context, params ListParams) (_ *PageResult[account.Account], err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("account%s", params.QueryString())) //nolint:perfsprint // project convention: use fmt.Sprintf
	if err != nil {
		return nil, fmt.Errorf("%w: ListAccounts: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "account")
	}

	var page PageResult[account.Account]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// Authenticate validates credentials and returns a session token on success.
func (c *SystemdClient) Authenticate(ctx context.Context, username, password string) (_ *AuthenticateResponse, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AuthenticateRequest{Username: username, Password: password})

	resp, err := c.postJSON(ctx, "account/authenticate", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: Authenticate: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "account/authenticate")
	}

	var authResp AuthenticateResponse
	return &authResp, json.NewDecoder(resp.Body).Decode(&authResp)
}

// RevokeSession invalidates a session by its ID.
func (c *SystemdClient) RevokeSession(ctx context.Context, sessionID string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RevokeSessionRequest{SessionID: sessionID})

	return c.postClient(ctx, "account/session/revoke", pr)
}

// ListSessions returns all active sessions for the user identified by the
// given bearer token.
func (c *SystemdClient) ListSessions(ctx context.Context, token string) (_ []account.Session, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.route("account/sessions"), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: ListSessions: %w", ErrNewRequest, err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token)) //nolint:perfsprint // project convention: use fmt.Sprintf

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: ListSessions: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "account/sessions")
	}

	var sessions []account.Session
	return sessions, json.NewDecoder(resp.Body).Decode(&sessions)
}

// SessionUsername returns the username associated with the given session token.
func (c *SystemdClient) SessionUsername(ctx context.Context, token string) (_ string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.route("/account/me"), nil)
	if err != nil {
		return "", fmt.Errorf("%w: SessionUsername: %w", ErrNewRequest, err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token)) //nolint:perfsprint // project convention: use fmt.Sprintf

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: SessionUsername: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return "", readProblemDetail(resp, "GET", "/account/me")
	}

	var result SessionUsernameResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", err
	}
	return result.Username, nil
}

// --- Audit ---

// ListAuditLog returns a paginated audit log. The opts parameter controls
// filtering (by account, before_id) and page size. The token parameter
// provides the bearer token for authentication.
func (c *SystemdClient) ListAuditLog(ctx context.Context, opts account.AuditListOptions, token string) (_ *account.AuditPage, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, opts)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route("audit/log"), pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListAuditLog: %w", ErrNewRequest, err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token)) //nolint:perfsprint // project convention: use fmt.Sprintf
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: ListAuditLog: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "audit/log")
	}

	var page account.AuditPage
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// --- Settings ---

// GetSettings returns all system settings as a key-value map.
func (c *SystemdClient) GetSettings(ctx context.Context) (_ map[string]string, err error) {
	resp, err := c.getClient(ctx, "settings")
	if err != nil {
		return nil, fmt.Errorf("%w: GetSettings: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "settings")
	}

	var settings map[string]string
	return settings, json.NewDecoder(resp.Body).Decode(&settings)
}

// GetSetting returns the value of a single system setting by key.
func (c *SystemdClient) GetSetting(ctx context.Context, key string) (_ string, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, GetSettingRequest{Key: key})

	resp, err := c.postJSON(ctx, "settings/get", pr)
	if err != nil {
		return "", fmt.Errorf("%w: GetSetting: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return "", readProblemDetail(resp, "POST", "settings/get")
	}

	var result struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", err
	}
	return result.Value, nil
}

// SetSetting creates or updates a system setting.
func (c *SystemdClient) SetSetting(ctx context.Context, key, value string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, SetSettingRequest{Key: key, Value: value})

	return c.postClient(ctx, "settings/set", pr)
}

// --- Upgrades ---

// ListUpgrades returns packages that have newer versions available in their
// source repositories.
func (c *SystemdClient) ListUpgrades(ctx context.Context) (_ []PackageUpgrade, err error) {
	resp, err := c.getClient(ctx, "packages/upgrades")
	if err != nil {
		return nil, fmt.Errorf("%w: ListUpgrades: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "packages/upgrades")
	}

	var upgrades []PackageUpgrade
	return upgrades, json.NewDecoder(resp.Body).Decode(&upgrades)
}

// DismissUpgrades marks all pending upgrade notifications as dismissed.
func (c *SystemdClient) DismissUpgrades(ctx context.Context) error {
	return c.postClient(ctx, "packages/upgrades/dismiss", nil)
}

// --- Archive ---

// UploadArchive uploads and extracts an archive into the named subvolume.
// Supported formats are tar.gz, tar.bz2, and tar.xz (detected from the
// filename extension). The archive data is read from archiveReader and sent
// as a multipart form upload. When subpath is non-empty, extraction is
// limited to that directory within the subvolume. When stopService is
// non-empty, the named systemd service is stopped before extraction and
// restarted afterward.
func (c *SystemdClient) UploadArchive(ctx context.Context, subvolume string, archiveReader io.Reader, filename, subpath, stopService string) (_ *ArchiveUploadResponse, err error) {
	pr, pw := io.Pipe()

	writer := multipart.NewWriter(pw)
	go func() {
		if err := writer.WriteField("subvolume", subvolume); err != nil {
			pw.CloseWithError(err)
			return
		}
		if subpath != "" {
			if err := writer.WriteField("subpath", subpath); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		if stopService != "" {
			if err := writer.WriteField("stop_service", stopService); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		part, err := writer.CreateFormFile("archive", filename)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, archiveReader); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.CloseWithError(writer.Close())
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route("storage/upload-archive"), pr)
	if err != nil {
		return nil, fmt.Errorf("%w: POST storage/upload-archive: %w", ErrNewRequest, err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if c.Token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token)) //nolint:perfsprint // project convention
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: POST storage/upload-archive: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "POST", "storage/upload-archive")
	}

	var result ArchiveUploadResponse
	return &result, json.NewDecoder(resp.Body).Decode(&result)
}

// DownloadArchive creates an archive of the specified paths within the named
// subvolume and returns a reader for the archive data. The format parameter
// selects the compression: "tar.gz", "tar.bz2", or "tar.xz". When stopService
// is non-empty, the named systemd service is stopped during archival. The
// caller must close the returned [io.ReadCloser].
func (c *SystemdClient) DownloadArchive(ctx context.Context, subvolume string, paths []string, stopService, format string) (_ io.ReadCloser, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, DownloadArchiveRequest{Subvolume: subvolume, Paths: paths, StopService: stopService, Format: format})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.route("storage/download-archive"), pr)
	if err != nil {
		return nil, fmt.Errorf("%w: POST storage/download-archive: %w", ErrNewRequest, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token)) //nolint:perfsprint // project convention
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: POST storage/download-archive: %w", ErrHTTPRequest, err)
	}

	if resp.StatusCode != http.StatusOK {
		defer func() {
			err = errors.Join(err, resp.Body.Close())
		}()
		return nil, readProblemDetail(resp, "POST", "storage/download-archive")
	}

	return resp.Body, nil
}

// --- Status ---

// Ping returns service health status and summary counts for filesystems,
// repositories, packages, accounts, and units.
func (c *SystemdClient) Ping(ctx context.Context) (_ *PingResponse, err error) {
	resp, err := c.getClient(ctx, "status/ping")
	if err != nil {
		return nil, fmt.Errorf("%w: Ping: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, readProblemDetail(resp, "GET", "status/ping")
	}

	var ping PingResponse
	return &ping, json.NewDecoder(resp.Body).Decode(&ping)
}
