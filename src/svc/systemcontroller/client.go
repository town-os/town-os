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
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

type Client interface {
	CreateFilesystem(storage.Filesystem) error
	ModifyFilesystem(string, storage.Filesystem) error
	RemoveFilesystem(string) error
	ListFilesystems(string) ([]storage.Filesystem, error)

	AddRepository(string) error
	RemoveRepository(string) error
	ListRepositories() ([]RepositoryInfo, error)

	ListPackages() ([]string, error)
	GetPackageQuestions(string) (map[string]packages.Question, error)

	InstallPackage(name, version string, responses packages.Responses) error
	UninstallPackage(name, version string) error
	ListInstalled() ([]string, error)
	GetResponses(name, version string) (packages.Responses, error)

	ListUnits() ([]systemd.UnitStatus, error)
	SetUnitStatus(name string, action systemd.StatusAction) error
	LogReplay(name string) (<-chan systemd.JournalEntry, error)

	CreateAccount(username, password, email, phone, realName string, admin bool) (*account.Account, error)
	GetAccount(username string) (*account.Account, error)
	UpdateAccount(username string, fields account.UpdateFields) (*account.Account, error)
	DeleteAccount(username string) error
	ListAccounts() ([]account.Account, error)
	Authenticate(username, password string) (*AuthenticateResponse, error)
	RevokeSession(sessionID string) error
	ListSessions(token string) ([]account.Session, error)
	SessionUsername(token string) (string, error)

	Ping() (*PingResponse, error)
}

type SystemdClient struct {
	HTTP    *http.Client
	BaseURL string
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

func (c *SystemdClient) route(path string) string {
	result, err := url.JoinPath(c.BaseURL, path)
	if err != nil {
		return fmt.Sprintf("%s/%s", c.BaseURL, path)
	}
	return result
}

func pipeEncode(pw *io.PipeWriter, v any) {
	pw.CloseWithError(json.NewEncoder(pw).Encode(v))
}

func (c *SystemdClient) postClient(path string, body io.Reader) (err error) {
	resp, err := c.HTTP.Post(c.route(path), "application/json", body)
	if err != nil {
		return fmt.Errorf("http error in POST %s: %v", path, err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return fmt.Errorf("unsuccessful status code in POST %s: %v", path, resp.StatusCode)
	}

	return nil
}

// --- Storage ---

func (c *SystemdClient) CreateFilesystem(fs storage.Filesystem) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, fs)

	return c.postClient("storage/create", pr)
}

func (c *SystemdClient) ModifyFilesystem(name string, fs storage.Filesystem) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, ModifyFilesystemRequest{Name: name, Filesystem: fs})

	return c.postClient("storage/modify", pr)
}

func (c *SystemdClient) RemoveFilesystem(name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, FilesystemName{Name: name})

	return c.postClient("storage/remove", pr)
}

func (c *SystemdClient) ListFilesystems(prefix string) (_ []storage.Filesystem, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, FilesystemName{Name: prefix})

	resp, err := c.HTTP.Post(c.route("storage"), "application/json", pr)
	if err != nil {
		return nil, fmt.Errorf("http error in ListFilesystems: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in ListFilesystems: %v", resp.StatusCode)
	}

	de := json.NewDecoder(resp.Body)
	fs := []storage.Filesystem{}

	return fs, de.Decode(&fs)
}

// --- Repository ---

func (c *SystemdClient) AddRepository(rawURL string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AddRepositoryRequest{URL: rawURL})

	return c.postClient("repository/add", pr)
}

func (c *SystemdClient) RemoveRepository(name string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RepositoryNameRequest{Name: name})

	return c.postClient("repository/remove", pr)
}

func (c *SystemdClient) ListRepositories() (_ []RepositoryInfo, err error) {
	resp, err := c.HTTP.Get(c.route("repository"))
	if err != nil {
		return nil, fmt.Errorf("http error in ListRepositories: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in ListRepositories: %v", resp.StatusCode)
	}

	de := json.NewDecoder(resp.Body)
	var repos []RepositoryInfo

	return repos, de.Decode(&repos)
}

// --- Packages ---

func (c *SystemdClient) ListPackages() (_ []string, err error) {
	resp, err := c.HTTP.Get(c.route("packages"))
	if err != nil {
		return nil, fmt.Errorf("http error in ListPackages: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in ListPackages: %v", resp.StatusCode)
	}

	de := json.NewDecoder(resp.Body)
	var pkgs []string

	return pkgs, de.Decode(&pkgs)
}

func (c *SystemdClient) GetPackageQuestions(name string) (_ map[string]packages.Question, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, PackageNameRequest{Name: name})

	resp, err := c.HTTP.Post(c.route("packages/questions"), "application/json", pr)
	if err != nil {
		return nil, fmt.Errorf("http error in GetPackageQuestions: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in GetPackageQuestions: %v", resp.StatusCode)
	}

	de := json.NewDecoder(resp.Body)
	var questions map[string]packages.Question

	return questions, de.Decode(&questions)
}

// --- Install ---

func (c *SystemdClient) InstallPackage(name, version string, responses packages.Responses) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, InstallRequest{Name: name, Version: version, Responses: responses})

	return c.postClient("packages/install", pr)
}

func (c *SystemdClient) UninstallPackage(name, version string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, UninstallRequest{Name: name, Version: version})

	return c.postClient("packages/uninstall", pr)
}

func (c *SystemdClient) ListInstalled() (_ []string, err error) {
	resp, err := c.HTTP.Get(c.route("packages/installed"))
	if err != nil {
		return nil, fmt.Errorf("http error in ListInstalled: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in ListInstalled: %v", resp.StatusCode)
	}

	de := json.NewDecoder(resp.Body)
	var pkgs []string

	return pkgs, de.Decode(&pkgs)
}

func (c *SystemdClient) GetResponses(name, version string) (_ packages.Responses, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, GetResponsesRequest{Name: name, Version: version})

	resp, err := c.HTTP.Post(c.route("packages/responses"), "application/json", pr)
	if err != nil {
		return nil, fmt.Errorf("http error in GetResponses: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in GetResponses: %v", resp.StatusCode)
	}

	de := json.NewDecoder(resp.Body)
	var responses packages.Responses

	return responses, de.Decode(&responses)
}

// --- Systemd ---

func (c *SystemdClient) ListUnits() (_ []systemd.UnitStatus, err error) {
	resp, err := c.HTTP.Get(c.route("systemd/units"))
	if err != nil {
		return nil, fmt.Errorf("http error in ListUnits: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in ListUnits: %v", resp.StatusCode)
	}

	de := json.NewDecoder(resp.Body)
	var units []systemd.UnitStatus

	return units, de.Decode(&units)
}

func (c *SystemdClient) SetUnitStatus(name string, action systemd.StatusAction) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, SetStatusRequest{Name: name, Action: action})

	return c.postClient("systemd/status", pr)
}

func (c *SystemdClient) LogReplay(name string) (_ <-chan systemd.JournalEntry, err error) {
	resp, err := c.HTTP.Get(c.route("systemd/logs") + "?unit=" + url.QueryEscape(name))
	if err != nil {
		return nil, fmt.Errorf("http error in LogReplay: %v", err)
	}

	if resp.StatusCode != 200 {
		if cerr := resp.Body.Close(); cerr != nil {
			return nil, fmt.Errorf("unsuccessful status code in LogReplay: %v (close: %v)", resp.StatusCode, cerr)
		}
		return nil, fmt.Errorf("unsuccessful status code in LogReplay: %v", resp.StatusCode)
	}

	ch := make(chan systemd.JournalEntry)
	go func() {
		defer close(ch)
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil && err == nil {
				err = cerr
			}
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

// --- Account ---

func (c *SystemdClient) CreateAccount(username, password, email, phone, realName string, admin bool) (_ *account.Account, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, CreateAccountRequest{
		Username: username, Password: password,
		Email: email, Phone: phone, RealName: realName, Admin: admin,
	})

	resp, err := c.HTTP.Post(c.route("account/create"), "application/json", pr)
	if err != nil {
		return nil, fmt.Errorf("http error in CreateAccount: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in CreateAccount: %v", resp.StatusCode)
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

func (c *SystemdClient) GetAccount(username string) (_ *account.Account, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, GetAccountRequest{Username: username})

	resp, err := c.HTTP.Post(c.route("account"), "application/json", pr)
	if err != nil {
		return nil, fmt.Errorf("http error in GetAccount: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in GetAccount: %v", resp.StatusCode)
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

func (c *SystemdClient) UpdateAccount(username string, fields account.UpdateFields) (_ *account.Account, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, UpdateAccountRequest{Username: username, Fields: fields})

	resp, err := c.HTTP.Post(c.route("account/update"), "application/json", pr)
	if err != nil {
		return nil, fmt.Errorf("http error in UpdateAccount: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in UpdateAccount: %v", resp.StatusCode)
	}

	var acct account.Account
	return &acct, json.NewDecoder(resp.Body).Decode(&acct)
}

func (c *SystemdClient) DeleteAccount(username string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, DeleteAccountRequest{Username: username})

	return c.postClient("account/delete", pr)
}

func (c *SystemdClient) ListAccounts() (_ []account.Account, err error) {
	resp, err := c.HTTP.Get(c.route("account"))
	if err != nil {
		return nil, fmt.Errorf("http error in ListAccounts: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in ListAccounts: %v", resp.StatusCode)
	}

	var accounts []account.Account
	return accounts, json.NewDecoder(resp.Body).Decode(&accounts)
}

func (c *SystemdClient) Authenticate(username, password string) (_ *AuthenticateResponse, err error) {
	pr, pw := io.Pipe()
	go pipeEncode(pw, AuthenticateRequest{Username: username, Password: password})

	resp, err := c.HTTP.Post(c.route("account/authenticate"), "application/json", pr)
	if err != nil {
		return nil, fmt.Errorf("http error in Authenticate: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in Authenticate: %v", resp.StatusCode)
	}

	var authResp AuthenticateResponse
	return &authResp, json.NewDecoder(resp.Body).Decode(&authResp)
}

func (c *SystemdClient) RevokeSession(sessionID string) error {
	pr, pw := io.Pipe()
	go pipeEncode(pw, RevokeSessionRequest{SessionID: sessionID})

	return c.postClient("account/session/revoke", pr)
}

func (c *SystemdClient) ListSessions(token string) (_ []account.Session, err error) {
	req, err := http.NewRequest("GET", c.route("account/sessions"), nil)
	if err != nil {
		return nil, fmt.Errorf("new request in ListSessions: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http error in ListSessions: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in ListSessions: %v", resp.StatusCode)
	}

	var sessions []account.Session
	return sessions, json.NewDecoder(resp.Body).Decode(&sessions)
}

func (c *SystemdClient) SessionUsername(token string) (_ string, err error) {
	req, err := http.NewRequest("GET", c.route("account/session/username"), nil)
	if err != nil {
		return "", fmt.Errorf("new request in SessionUsername: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("http error in SessionUsername: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("unsuccessful status code in SessionUsername: %v", resp.StatusCode)
	}

	var result SessionUsernameResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Username, nil
}

// --- Status ---

func (c *SystemdClient) Ping() (_ *PingResponse, err error) {
	resp, err := c.HTTP.Get(c.route("status/ping"))
	if err != nil {
		return nil, fmt.Errorf("http error in Ping: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in Ping: %v", resp.StatusCode)
	}

	var ping PingResponse
	return &ping, json.NewDecoder(resp.Body).Decode(&ping)
}
