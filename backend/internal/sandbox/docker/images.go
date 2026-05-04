package docker

import (
	"context"
	"io"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/errdefs"
)

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.raw.Ping(ctx)
	return err
}

func (c *Client) ImageExists(ctx context.Context, ref string) (bool, error) {
	_, _, err := c.raw.ImageInspectWithRaw(ctx, ref)
	if err == nil {
		return true, nil
	}
	if errdefs.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

func (c *Client) PullImage(ctx context.Context, ref string) error {
	reader, err := c.raw.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	_, err = io.Copy(io.Discard, reader)
	return err
}
