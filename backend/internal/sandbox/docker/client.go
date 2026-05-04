package docker

import "github.com/docker/docker/client"

type Client struct {
	raw *client.Client
}

func NewClient() (*Client, error) {
	raw, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Client{raw: raw}, nil
}

func (c *Client) Close() error {
	if c == nil || c.raw == nil {
		return nil
	}
	return c.raw.Close()
}
