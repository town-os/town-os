package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

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
	return fmt.Sprintf("%s/%s", c.BaseURL, path)
}

func (c *SystemClient) postClient(path string, payload []byte) error {
	resp, err := c.HTTP.Post(c.route(path), "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("http error in POST %s: %v", path, err)
	}
	defer resp.Body.Close()

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

func (c *SystemClient) ListFilesystems(prefix string) ([]storage.Filesystem, error) {
	payload, err := json.Marshal(FilesystemName{Name: prefix})
	if err != nil {
		return nil, err
	}

	resp, err := c.HTTP.Post(c.route("storage"), "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("http error in ListFilesystems: %v", err)
	}
	defer resp.Body.Close()

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

func (c *SystemClient) ListRepositories() ([]RepositoryInfo, error) {
	resp, err := c.HTTP.Post(c.route("repository"), "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("http error in ListRepositories: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in ListRepositories: %v", resp.StatusCode)
	}

	de := json.NewDecoder(resp.Body)
	var repos []RepositoryInfo

	return repos, de.Decode(&repos)
}

// --- Packages ---

func (c *SystemClient) ListPackages() ([]string, error) {
	resp, err := c.HTTP.Post(c.route("packages"), "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("http error in ListPackages: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unsuccessful status code in ListPackages: %v", resp.StatusCode)
	}

	de := json.NewDecoder(resp.Body)
	var pkgs []string

	return pkgs, de.Decode(&pkgs)
}
