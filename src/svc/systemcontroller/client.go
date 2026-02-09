package systemcontroller

import (
	"context"
	"net"
	"net/http"
	"time"
)

type SystemClient struct {
	HTTP *http.Client
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

	return &SystemClient{HTTP: client}, nil
}

func FromClient(client *http.Client) (*SystemClient, error) {
	return &SystemClient{HTTP: client}, nil
}
