package docker

import (
	"context"
	"fmt"
	"github.com/docker/docker/client"
	"reflow/internal/util"
)

var dockerClient *client.Client

// GetClient initializes and returns a Docker API client.
func GetClient() (*client.Client, error) {
	if dockerClient != nil {
		return dockerClient, nil
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		util.Log.Errorf("Failed to create Docker client: %v", err)
		return nil, fmt.Errorf("failed to create Docker client: %w. Is Docker running and accessible", err)
	}

	ctx := context.Background()
	_, err = cli.Ping(ctx)
	if err != nil {
		util.Log.Errorf("Failed to ping Docker daemon: %v", err)
		return nil, fmt.Errorf("failed to connect to Docker daemon: %w. Is Docker running", err)
	}

	util.Log.Debug("Docker client initialized successfully.")
	dockerClient = cli
	return dockerClient, nil
}
