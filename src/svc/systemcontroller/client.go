package systemcontroller

import (
	"bufio"
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

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

type Client interface {
	CreateFilesystem(ctx context.Context, fs storage.Filesystem) error
	ModifyFilesystem(ctx context.Context, name string, fs storage.Filesystem) error
	RemoveFilesystem(ctx context.Context, name string) error
	ListFilesystems(ctx context.Context, prefix string) ([]storage.Filesystem, error)

	AddRepository(ctx context.Context, name, rawURL, username, password string) error
	RemoveRepository(ctx context.Context, name string) error
	RefreshRepositories(ctx context.Context) (map[string]string, error)
	ListRepositories(ctx context.Context, params ListParams) (*PageResult[RepositoryInfo], error)

	ListPackages(ctx context.Context, params ListParams) (*PageResult[string], error)
	GetPackageQuestions(ctx context.Context, name string) (map[string]packages.Question, error)

	InstallPackage(ctx context.Context, name, version string, responses packages.Responses) error
	UninstallPackage(ctx context.Context, name, version string) error
	ListInstalled(ctx context.Context, params ListParams) (*PageResult[string], error)
	GetResponses(ctx context.Context, name, version string) (packages.Responses, error)

	ListUnits(ctx context.Context, params ListParams) (*PageResult[systemd.UnitStatus], error)
	SetUnitStatus(ctx context.Context, name string, action systemd.StatusAction) error
	LogReplay(ctx context.Context, name string) (<-chan systemd.JournalEntry, error)
	LogTail(ctx context.Context, params systemd.LogTailParams) (systemd.LogTailResult, error)

	CreateAccount(ctx context.Context, username, password, email, phone, realName string, admin bool) (*account.Account, error)
	GetAccount(ctx context.Context, username string) (*account.Account, error)
	UpdateAccount(ctx context.Context, username string, fields account.UpdateFields) (*account.Account, error)
	DisableAccount(ctx context.Context, username string) error
	EnableAccount(ctx context.Context, username string) error
	ListAccounts(ctx context.Context, sortBy, sortOrder string) ([]account.Account, error)
	Authenticate(ctx context.Context, username, password string) (*AuthenticateResponse, error)
	RevokeSession(ctx context.Context, sessionID string) error
	ListSessions(ctx context.Context, token string) ([]account.Session, error)
	SessionUsername(ctx context.Context, token string) (string, error)

	ListAuditLog(ctx context.Context, opts account.AuditListOptions, token string) (*account.AuditPage, error)

	Ping(ctx context.Context) (*PingResponse, error)
}

var (
	ErrNewRequest         = errors.New("new request")
	ErrHTTPRequest        = errors.New("http request")
	ErrUnsuccessfulStatus = errors.New("unsuccessful status code")
)

type SystemdClient struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
}

func InitClient(sock string) (*SystemdClient, error) {
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			return net.Dial("unix", sock)
		}},
		Timeout: 60 * time.Second,
	}

	return FromClient(client, "http://localhost")
}

func FromClient(client *http.Client, baseURL string) (*SystemdClient, error) {
	return &SystemdClient{HTTP: client, BaseURL: baseURL}, nil
}

func sortQueryString(sortBy, sortOrder string) string {
	params := url.Values{}
	if sortBy != "" {
		params.Set("sort_by", sortBy)
	}
	if sortOrder != "" {
		params.Set("sort_order", sortOrder)
	}
	if len(params) == 0 {
		return ""
	}
	return fmt.Sprintf("?%s", params.Encode())
}

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

func pipeEncode(pw *io.PipeWriter, v any) {
	pw.CloseWithError(json.NewEncoder(pw).Encode(v))
}

func (c *SystemdClient) postClient(ctx context.Context, path string, body io.Reader) (err error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.route(path), body)
	if err != nil {
		return fmt.Errorf("%w: POST %s: %w", ErrNewRequest, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: POST %s: %w", ErrHTTPRequest, path, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return readProblemDetail(resp, "POST", path)
	}

	return nil
}

func (c *SystemdClient) getClient(ctx context.Context, path string) (_ *http.Response, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.route(path), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: GET %s: %w", ErrNewRequest, path, err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	}
	return c.HTTP.Do(req)
}

func (c *SystemdClient) postJSON(ctx context.Context, path string, body io.Reader) (_ *http.Response, err error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.route(path), body)
	if err != nil {
		return nil, fmt.Errorf("%w: POST %s: %w", ErrNewRequest, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.Token))
	}
	return c.HTTP.Do(req)
}

// --- Storage ---

func (c *SystemdClient) CreateFilesystem(ctx context.Context, fs storage.Filesystem) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, fs)

	return c.postClient(ctx, "storage/create", pr)
}

func (c *SystemdClient) ModifyFilesystem(ctx context.Context, name string, fs storage.Filesystem) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, ModifyFilesystemRequest{Name: name, Filesystem: fs})

	return c.postClient(ctx, "storage/modify", pr)
}

func (c *SystemdClient) RemoveFilesystem(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, FilesystemName{Name: name})

	return c.postClient(ctx, "storage/remove", pr)
}

func (c *SystemdClient) ListFilesystems(ctx context.Context, prefix string) (_ []storage.Filesystem, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, FilesystemName{Name: prefix})

	resp, err := c.postJSON(ctx, "storage", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListFilesystems: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "POST", "storage")
	}

	fs := []storage.Filesystem{}
	return fs, json.NewDecoder(resp.Body).Decode(&fs)
}

// --- Repository ---

func (c *SystemdClient) AddRepository(ctx context.Context, name, rawURL, username, password string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AddRepositoryRequest{Name: name, URL: rawURL, Username: username, Password: password})

	return c.postClient(ctx, "repository/add", pr)
}

func (c *SystemdClient) RemoveRepository(ctx context.Context, name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RepositoryNameRequest{Name: name})

	return c.postClient(ctx, "repository/remove", pr)
}

func (c *SystemdClient) RefreshRepositories(ctx context.Context) (_ map[string]string, err error) {
	resp, err := c.postJSON(ctx, "repository/refresh", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: RefreshRepositories: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "POST", "repository/refresh")
	}

	var errs map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&errs); err != nil {
		// empty body means no errors
		return nil, nil
	}
	return errs, nil
}

func (c *SystemdClient) ListRepositories(ctx context.Context, params ListParams) (_ *PageResult[RepositoryInfo], err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("repository%s", params.QueryString()))
	if err != nil {
		return nil, fmt.Errorf("%w: ListRepositories: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "GET", "repository")
	}

	var page PageResult[RepositoryInfo]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// --- Packages ---

func (c *SystemdClient) ListPackages(ctx context.Context, params ListParams) (_ *PageResult[string], err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("packages%s", params.QueryString()))
	if err != nil {
		return nil, fmt.Errorf("%w: ListPackages: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "GET", "packages")
	}

	var page PageResult[string]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

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

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "POST", "packages/questions")
	}

	var questions map[string]packages.Question
	return questions, json.NewDecoder(resp.Body).Decode(&questions)
}

// --- Install ---

func (c *SystemdClient) InstallPackage(ctx context.Context, name, version string, responses packages.Responses) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, InstallRequest{Name: name, Version: version, Responses: responses})

	return c.postClient(ctx, "packages/install", pr)
}

func (c *SystemdClient) UninstallPackage(ctx context.Context, name, version string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, UninstallRequest{Name: name, Version: version})

	return c.postClient(ctx, "packages/uninstall", pr)
}

func (c *SystemdClient) ListInstalled(ctx context.Context, params ListParams) (_ *PageResult[string], err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("packages/installed%s", params.QueryString()))
	if err != nil {
		return nil, fmt.Errorf("%w: ListInstalled: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "GET", "packages/installed")
	}

	var page PageResult[string]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

func (c *SystemdClient) GetResponses(ctx context.Context, name, version string) (_ packages.Responses, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, GetResponsesRequest{Name: name, Version: version})

	resp, err := c.postJSON(ctx, "packages/responses", pr)
	if err != nil {
		return nil, fmt.Errorf("%w: GetResponses: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "POST", "packages/responses")
	}

	var responses packages.Responses
	return responses, json.NewDecoder(resp.Body).Decode(&responses)
}

// --- Systemd ---

func (c *SystemdClient) ListUnits(ctx context.Context, params ListParams) (_ *PageResult[systemd.UnitStatus], err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("systemd/units%s", params.QueryString()))
	if err != nil {
		return nil, fmt.Errorf("%w: ListUnits: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "GET", "systemd/units")
	}

	var page PageResult[systemd.UnitStatus]
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

func (c *SystemdClient) SetUnitStatus(ctx context.Context, name string, action systemd.StatusAction) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, SetStatusRequest{Name: name, Action: action})

	return c.postClient(ctx, "systemd/status", pr)
}

func (c *SystemdClient) LogReplay(ctx context.Context, name string) (_ <-chan systemd.JournalEntry, err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("systemd/logs?unit=%s", url.QueryEscape(name)))
	if err != nil {
		return nil, fmt.Errorf("%w: LogReplay: %w", ErrHTTPRequest, err)
	}

	if resp.StatusCode != 200 {
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
			if err := json.NewDecoder(strings.NewReader(strings.TrimPrefix(line, "data: "))).Decode(&entry); err != nil {
				return
			}
			ch <- entry
		}
	}()

	return ch, nil
}

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

	resp, err := c.getClient(ctx, q)
	if err != nil {
		return systemd.LogTailResult{}, fmt.Errorf("%w: LogTail: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return systemd.LogTailResult{}, readProblemDetail(resp, "GET", "systemd/logs/tail")
	}

	var result systemd.LogTailResult
	return result, json.NewDecoder(resp.Body).Decode(&result)
}

// --- Account ---

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

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "POST", "account/create")
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

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

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "POST", "account")
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

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

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "POST", "account/update")
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

func (c *SystemdClient) DisableAccount(ctx context.Context, username string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, DisableAccountRequest{Username: username})

	return c.postClient(ctx, "account/disable", pr)
}

func (c *SystemdClient) EnableAccount(ctx context.Context, username string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, EnableAccountRequest{Username: username})

	return c.postClient(ctx, "account/enable", pr)
}

func (c *SystemdClient) ListAccounts(ctx context.Context, sortBy, sortOrder string) (_ []account.Account, err error) {
	resp, err := c.getClient(ctx, fmt.Sprintf("account%s", sortQueryString(sortBy, sortOrder)))
	if err != nil {
		return nil, fmt.Errorf("%w: ListAccounts: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "GET", "account")
	}

	var accounts []account.Account
	return accounts, json.NewDecoder(resp.Body).Decode(&accounts)
}

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

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "POST", "account/authenticate")
	}

	var authResp AuthenticateResponse
	return &authResp, json.NewDecoder(resp.Body).Decode(&authResp)
}

func (c *SystemdClient) RevokeSession(ctx context.Context, sessionID string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RevokeSessionRequest{SessionID: sessionID})

	return c.postClient(ctx, "account/session/revoke", pr)
}

func (c *SystemdClient) ListSessions(ctx context.Context, token string) (_ []account.Session, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.route("account/sessions"), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: ListSessions: %w", ErrNewRequest, err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: ListSessions: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "GET", "account/sessions")
	}

	var sessions []account.Session
	return sessions, json.NewDecoder(resp.Body).Decode(&sessions)
}

func (c *SystemdClient) SessionUsername(ctx context.Context, token string) (_ string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.route("/account/me"), nil)
	if err != nil {
		return "", fmt.Errorf("%w: SessionUsername: %w", ErrNewRequest, err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: SessionUsername: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return "", readProblemDetail(resp, "GET", "/account/me")
	}

	var result SessionUsernameResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Username, nil
}

// --- Audit ---

func (c *SystemdClient) ListAuditLog(ctx context.Context, opts account.AuditListOptions, token string) (_ *account.AuditPage, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, opts)

	req, err := http.NewRequestWithContext(ctx, "POST", c.route("audit/log"), pr)
	if err != nil {
		return nil, fmt.Errorf("%w: ListAuditLog: %w", ErrNewRequest, err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: ListAuditLog: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "POST", "audit/log")
	}

	var page account.AuditPage
	return &page, json.NewDecoder(resp.Body).Decode(&page)
}

// --- Status ---

func (c *SystemdClient) Ping(ctx context.Context) (_ *PingResponse, err error) {
	resp, err := c.getClient(ctx, "status/ping")
	if err != nil {
		return nil, fmt.Errorf("%w: Ping: %w", ErrHTTPRequest, err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != 200 {
		return nil, readProblemDetail(resp, "GET", "status/ping")
	}

	var ping PingResponse
	return &ping, json.NewDecoder(resp.Body).Decode(&ping)
}
