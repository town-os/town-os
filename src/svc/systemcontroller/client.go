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
	CreateFilesystem(name string) error
	ModifyFilesystem(name string, fs storage.Filesystem) error
	RemoveFilesystem(name string) error
	ListFilesystems(prefix string) ([]storage.Filesystem, error)
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

func (c *SystemClient) CreateFilesystem(name string) error {
	payload, err := json.Marshal(FilesystemName{Name: name})
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
