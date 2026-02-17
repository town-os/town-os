package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
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
}

type SystemClient struct {
	HTTP    *http.Client
	BaseURL string
}

func InitClient(sock string) (*SystemClient, error) {
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

func FromClient(client *http.Client, baseURL string) (*SystemClient, error) {
	return &SystemClient{HTTP: client, BaseURL: baseURL}, nil
}

func (c *SystemClient) route(path string) string {
	result, err := url.JoinPath(c.BaseURL, path)
	if err != nil {
		return fmt.Sprintf("%s/%s", c.BaseURL, path)
	}
	return result
}

func (c *SystemClient) postClient(path string, payload []byte) (err error) {
	resp, err := c.HTTP.Post(c.route(path), "application/json", bytes.NewBuffer(payload))
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

func (c *SystemClient) CreateFilesystem(fs storage.Filesystem) error {
	payload, err := json.Marshal(fs)
	if err != nil {
		return err
	}

	return c.postClient("storage/create", payload)
}

func (c *SystemClient) ModifyFilesystem(name string, fs storage.Filesystem) error {
	payload, err := json.Marshal(ModifyFilesystemRequest{Name: name, Filesystem: fs})
	if err != nil {
		return err
	}

	return c.postClient("storage/modify", payload)
}

func (c *SystemClient) RemoveFilesystem(name string) error {
	payload, err := json.Marshal(FilesystemName{Name: name})
	if err != nil {
		return err
	}

	return c.postClient("storage/remove", payload)
}

func (c *SystemClient) ListFilesystems(prefix string) (_ []storage.Filesystem, err error) {
	payload, err := json.Marshal(FilesystemName{Name: prefix})
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTP.Post(c.route("storage"), "application/json", bytes.NewBuffer(payload))
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

func (c *SystemClient) AddRepository(rawURL string) error {
	payload, err := json.Marshal(AddRepositoryRequest{URL: rawURL})
	if err != nil {
		return err
	}

	return c.postClient("repository/add", payload)
}

func (c *SystemClient) RemoveRepository(name string) error {
	payload, err := json.Marshal(RepositoryNameRequest{Name: name})
	if err != nil {
		return err
	}

	return c.postClient("repository/remove", payload)
}

func (c *SystemClient) ListRepositories() (_ []RepositoryInfo, err error) {
	resp, err := c.HTTP.Post(c.route("repository"), "application/json", bytes.NewBuffer([]byte("{}")))
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

func (c *SystemClient) ListPackages() (_ []string, err error) {
	resp, err := c.HTTP.Post(c.route("packages"), "application/json", bytes.NewBuffer([]byte("{}")))
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

func (c *SystemClient) GetPackageQuestions(name string) (_ map[string]packages.Question, err error) {
	payload, err := json.Marshal(PackageNameRequest{Name: name})
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTP.Post(c.route("packages/questions"), "application/json", bytes.NewBuffer(payload))
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

func (c *SystemClient) InstallPackage(name, version string, responses packages.Responses) error {
	payload, err := json.Marshal(InstallRequest{Name: name, Version: version, Responses: responses})
	if err != nil {
		return err
	}

	return c.postClient("packages/install", payload)
}

func (c *SystemClient) UninstallPackage(name, version string) error {
	payload, err := json.Marshal(UninstallRequest{Name: name, Version: version})
	if err != nil {
		return err
	}

	return c.postClient("packages/uninstall", payload)
}

func (c *SystemClient) ListInstalled() (_ []string, err error) {
	resp, err := c.HTTP.Post(c.route("packages/installed"), "application/json", bytes.NewBuffer([]byte("{}")))
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

func (c *SystemClient) GetResponses(name, version string) (_ packages.Responses, err error) {
	payload, err := json.Marshal(GetResponsesRequest{Name: name, Version: version})
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTP.Post(c.route("packages/responses"), "application/json", bytes.NewBuffer(payload))
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
