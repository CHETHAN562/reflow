package docker

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	dockerAPIClient "github.com/docker/docker/client"
	"io"
	"io/ioutil"
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

// PullImage pulls a Docker image from a registry.
func PullImage(ctx context.Context, imageName string) error {
	cli, err := GetClient()
	if err != nil {
		return err
	}

	util.Log.Infof("Pulling image '%s'...", imageName)
	pullOptions := image.PullOptions{}
	reader, err := cli.ImagePull(ctx, imageName, pullOptions)
	if err != nil {
		util.Log.Errorf("Failed to start image pull for '%s': %v", imageName, err)
		return fmt.Errorf("failed to pull image '%s': %w", imageName, err)
	}
	defer func(reader io.ReadCloser) {
		err := reader.Close()
		if err != nil {
			util.Log.Errorf("Error closing image pull stream: %v", err)
		} else {
			util.Log.Debugf("Closed image pull stream successfully.")
		}
	}(reader)

	// Discard the output, but check for errors during the pull
	_, err = io.Copy(ioutil.Discard, reader)
	if err != nil {
		util.Log.Errorf("Error during image pull stream for '%s': %v", imageName, err)
		return fmt.Errorf("error reading image pull stream for '%s': %w", imageName, err)
	}

	util.Log.Infof("Successfully pulled image '%s' (or it was up-to-date).", imageName)
	return nil
}

// IsErrNotFound checks if a Docker error is a "not found" error.
func IsErrNotFound(err error) bool {
	return dockerAPIClient.IsErrNotFound(err)
}
