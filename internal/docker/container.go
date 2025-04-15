package docker

import (
	"context"
	"fmt"
	"io"
	"reflow/internal/util"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	dockerAPIClient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

const (
	LabelProject     = "reflow.project"
	LabelEnvironment = "reflow.environment"
	LabelSlot        = "reflow.slot"
	LabelCommit      = "reflow.commit"
	LabelManaged     = "reflow.managed"
)

// FindContainersByLabels finds containers matching a given set of labels.
func FindContainersByLabels(ctx context.Context, labels map[string]string) ([]types.Container, error) {
	cli, err := GetClient()
	if err != nil {
		return nil, err
	}

	filterArgs := filters.NewArgs()
	for k, v := range labels {
		filterArgs.Add("label", fmt.Sprintf("%s=%s", k, v))
	}
	filterArgs.Add("label", fmt.Sprintf("%s=true", LabelManaged))

	util.Log.Debugf("Finding containers with filters: %s", filterArgs.Get("label"))

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
		All:     true,
	})
	if err != nil {
		util.Log.Errorf("Failed to list containers with filters %v: %v", filterArgs, err)
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	util.Log.Debugf("Found %d container(s) matching labels: %v", len(containers), labels)
	return containers, nil
}

// ListManagedContainers lists all containers managed by Reflow.
func ListManagedContainers(ctx context.Context) ([]types.Container, error) {
	cli, err := GetClient()
	if err != nil {
		return nil, err
	}

	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("%s=true", LabelManaged))

	util.Log.Debugf("Listing all containers managed by Reflow (label: %s=true)", LabelManaged)

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
		All:     true,
	})
	if err != nil {
		util.Log.Errorf("Failed to list managed containers: %v", err)
		return nil, fmt.Errorf("failed to list managed containers: %w", err)
	}

	util.Log.Debugf("Found %d managed container(s).", len(containers))
	return containers, nil
}

// InspectContainer gets detailed information about a single container.
func InspectContainer(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	cli, err := GetClient()
	if err != nil {
		return types.ContainerJSON{}, err
	}
	util.Log.Debugf("Inspecting container %s...", containerID)

	inspectData, err := cli.ContainerInspect(ctx, containerID)
	if err != nil {
		if IsErrNotFound(err) {
			util.Log.Warnf("Container %s not found for inspect.", containerID)
		} else {
			util.Log.Errorf("Failed to inspect container %s: %v", containerID, err)
		}
		// Return the original error for handlers to check IsErrNotFound
		return types.ContainerJSON{}, err
	}

	return inspectData, nil
}

// GetContainerStatusString provides a user-friendly status string.
func GetContainerStatusString(c types.Container) string {
	if c.ID == "" {
		return "Not Found"
	}
	if strings.ToLower(c.State) == "running" {
		return fmt.Sprintf("Running (%s)", c.Status)
	}
	return c.Status
}

// StopContainer stops a container by its ID.
func StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	cli, err := GetClient()
	if err != nil {
		return err
	}
	util.Log.Infof("Stopping container %s...", containerID[:12])

	var stopOptions container.StopOptions
	if timeout != nil {
		seconds := int(timeout.Seconds())
		stopOptions.Timeout = &seconds
	}

	err = cli.ContainerStop(ctx, containerID, stopOptions)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "container not running") {
			util.Log.Warnf("Container %s was already stopped.", containerID[:12])
			return nil
		}
		util.Log.Errorf("Failed to stop container %s: %v", containerID[:12], err)
		return fmt.Errorf("failed to stop container %s: %w", containerID[:12], err)
	}
	util.Log.Infof("Container %s stopped.", containerID[:12])
	return nil
}

// StartContainer starts a container by its ID.
func StartContainer(ctx context.Context, containerID string) error {
	// Get client explicitly
	cli, err := GetClient()
	if err != nil {
		return err
	}
	util.Log.Infof("Starting container %s...", containerID[:12])

	startOptions := container.StartOptions{}
	err = cli.ContainerStart(ctx, containerID, startOptions)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "is already started") || strings.Contains(strings.ToLower(err.Error()), "container already running") {
			util.Log.Warnf("Container %s was already running.", containerID[:12])
			return nil
		}
		util.Log.Errorf("Failed to start container %s: %v", containerID[:12], err)
		return fmt.Errorf("failed to start container %s: %w", containerID[:12], err)
	}
	util.Log.Infof("Container %s started.", containerID[:12])
	return nil
}

// RestartContainer restarts a container by ID.
func RestartContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	cli, err := GetClient()
	if err != nil {
		return err
	}
	util.Log.Infof("Restarting container %s...", containerID[:min(12, len(containerID))])

	var restartOptions container.StopOptions
	if timeout != nil {
		seconds := int(timeout.Seconds())
		restartOptions.Timeout = &seconds
	} else {
		defaultTimeoutSeconds := 10
		restartOptions.Timeout = &defaultTimeoutSeconds
	}

	err = cli.ContainerRestart(ctx, containerID, restartOptions)
	if err != nil {
		if IsErrNotFound(err) {
			util.Log.Errorf("Container %s not found, cannot restart.", containerID[:min(12, len(containerID))])
			return fmt.Errorf("container %s not found", containerID[:min(12, len(containerID))])
		}
		util.Log.Errorf("Failed to restart container %s: %v", containerID[:min(12, len(containerID))], err)
		return fmt.Errorf("failed to restart container %s: %w", containerID[:min(12, len(containerID))], err)
	}

	util.Log.Infof("Container %s restart initiated.", containerID[:min(12, len(containerID))])
	return nil
}

// RemoveContainer removes a container by ID. Assumes container is stopped.
func RemoveContainer(ctx context.Context, containerID string) error {
	cli, err := GetClient()
	if err != nil {
		return err
	}
	util.Log.Infof("Removing container %s...", containerID[:12])
	options := container.RemoveOptions{
		Force: false,
	}
	err = cli.ContainerRemove(ctx, containerID, options)
	if err != nil {
		if dockerAPIClient.IsErrNotFound(err) {
			util.Log.Warnf("Container %s not found, cannot remove.", containerID[:12])
			return nil
		}
		util.Log.Errorf("Failed to remove container %s: %v", containerID[:12], err)
		return fmt.Errorf("failed to remove container %s: %w", containerID[:12], err)
	}
	util.Log.Infof("Container %s removed.", containerID[:12])
	return nil
}

// ContainerRunOptions defines parameters for RunContainer.
type ContainerRunOptions struct {
	ImageName     string
	ContainerName string
	NetworkName   string
	Labels        map[string]string
	EnvVars       []string
	AppPort       int
	RestartPolicy string
}

// RunContainer creates and starts a container based on provided options.
func RunContainer(ctx context.Context, options ContainerRunOptions) (string, error) {
	cli, err := GetClient()
	if err != nil {
		return "", err
	}

	util.Log.Infof("Preparing to run container '%s' from image '%s'", options.ContainerName, options.ImageName)

	containerConfig := &container.Config{
		Image:  options.ImageName,
		Labels: options.Labels,
		Env:    options.EnvVars,
		ExposedPorts: nat.PortSet{
			nat.Port(fmt.Sprintf("%d/tcp", options.AppPort)): struct{}{},
		},
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{},
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyMode(options.RestartPolicy),
		},
	}

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			options.NetworkName: {},
		},
	}

	util.Log.Infof("Creating container '%s'...", options.ContainerName)
	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, nil, options.ContainerName)
	if err != nil {
		if strings.Contains(err.Error(), "is already in use by container") {
			util.Log.Warnf("Container name '%s' conflict during creation. Check for leftovers.", options.ContainerName)
			existing, inspectErr := cli.ContainerInspect(ctx, options.ContainerName)
			if inspectErr == nil {
				return existing.ID, fmt.Errorf("container named '%s' already exists (ID: %s)", options.ContainerName, existing.ID[:12])
			}
			return "", fmt.Errorf("container named '%s' already exists, but failed to inspect: %w", options.ContainerName, err)
		}
		util.Log.Errorf("Failed to create container '%s': %v", options.ContainerName, err)
		return "", fmt.Errorf("failed to create container '%s': %w", options.ContainerName, err)
	}
	containerID := resp.ID
	util.Log.Debugf("Container '%s' created with ID: %s", options.ContainerName, containerID)

	util.Log.Infof("Starting container '%s'...", options.ContainerName)
	startOptions := container.StartOptions{}
	if err := cli.ContainerStart(ctx, containerID, startOptions); err != nil {
		util.Log.Errorf("Failed to start container '%s': %v", options.ContainerName, err)
		rmErr := RemoveContainer(context.Background(), containerID)
		if rmErr != nil {
			util.Log.Warnf("Failed to clean up container %s after start failure: %v", containerID[:12], rmErr)
		}
		return "", fmt.Errorf("failed to start container '%s': %w", options.ContainerName, err)
	}

	util.Log.Infof("Container '%s' started successfully.", options.ContainerName)
	return containerID, nil
}

// GetContainerLogs fetches logs for a specific container.
func GetContainerLogs(ctx context.Context, containerID string, follow bool, tail string) (io.ReadCloser, error) {
	cli, err := GetClient()
	if err != nil {
		return nil, err
	}

	logOptions := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Timestamps: true,
	}

	util.Log.Debugf("Getting logs for container %s (Follow: %v, Tail: %s)", containerID[:12], follow, tail)
	logReader, err := cli.ContainerLogs(ctx, containerID, logOptions)
	if err != nil {
		util.Log.Errorf("Failed to get logs for container %s: %v", containerID[:12], err)
		return nil, fmt.Errorf("failed to get logs for container %s: %w", containerID[:12], err)
	}

	return logReader, nil
}
