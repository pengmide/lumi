package sandbox

import (
	"context"

	"github.com/docker/docker/api/types"
	sandboxdocker "github.com/pengmide/lumi/internal/sandbox/docker"
)

type dockerClient interface {
	Close() error
	CreateContainer(ctx context.Context, spec sandboxdocker.ContainerSpec) (string, error)
	ImageExists(ctx context.Context, ref string) (bool, error)
	InspectContainer(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ListSandboxContainers(ctx context.Context) ([]types.Container, error)
	Ping(ctx context.Context) error
	PullImage(ctx context.Context, ref string) error
	StartContainer(ctx context.Context, containerID string) error
	StopRemoveContainer(ctx context.Context, containerID string) error
}
