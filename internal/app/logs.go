package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"io"
	"os"
	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/util"
	"strings"
)

// StreamAppLogs fetches and streams logs for the active container of a specific project environment.
func StreamAppLogs(ctx context.Context, reflowBasePath, projectName, env string, follow bool, tail string) error {
	util.Log.Debugf("Attempting to get logs for project '%s', environment '%s'...", projectName, env)

	projState, err := config.LoadProjectState(reflowBasePath, projectName)
	if err != nil {
		return fmt.Errorf("failed to load project state for '%s': %w", projectName, err)
	}

	var activeSlot string
	var activeCommit string
	if env == "test" {
		activeSlot = projState.Test.ActiveSlot
		activeCommit = projState.Test.ActiveCommit
	} else if env == "prod" {
		activeSlot = projState.Prod.ActiveSlot
		activeCommit = projState.Prod.ActiveCommit
	} else {
		return fmt.Errorf("invalid environment specified: %s", env)
	}

	if activeCommit == "" || activeSlot == "" {
		return fmt.Errorf("no active deployment found in state for project '%s', environment '%s'. Cannot get logs", projectName, env)
	}

	util.Log.Debugf("Looking for active container: project=%s, env=%s, slot=%s", projectName, env, activeSlot)

	labels := map[string]string{
		docker.LabelProject:     projectName,
		docker.LabelEnvironment: env,
		docker.LabelSlot:        activeSlot,
	}

	containers, err := docker.FindContainersByLabels(ctx, labels)
	if err != nil {
		return fmt.Errorf("failed to find containers for project '%s' env '%s' slot '%s': %w", projectName, env, activeSlot, err)
	}

	var targetContainer *container.Summary = nil
	for i := range containers {
		c := containers[i]
		if c.State == "running" {
			if targetContainer != nil {
				util.Log.Errorf("Found multiple RUNNING containers for project '%s' env '%s' slot '%s'!", projectName, env, activeSlot)
				return fmt.Errorf("ambiguity: found multiple running containers for active slot")
			}
			targetContainer = &c
		}
	}

	if targetContainer == nil {
		if follow {
			return fmt.Errorf("cannot follow logs: no running container found for project '%s' env '%s' slot '%s'", projectName, env, activeSlot)
		}
		var latestExitedContainer *container.Summary = nil
		for i := range containers {
			c := containers[i]
			if c.State == "exited" {
				if latestExitedContainer == nil || c.Created > latestExitedContainer.Created {
					latestExitedContainer = &c
				}
			}
		}
		if latestExitedContainer == nil {
			return fmt.Errorf("no running or recently stopped container found for project '%s' env '%s' slot '%s'", projectName, env, activeSlot)
		}
		util.Log.Warnf("Active container is stopped. Showing logs for last exited container: %s", latestExitedContainer.ID[:12])
		targetContainer = latestExitedContainer
	}

	containerID := targetContainer.ID
	containerName := strings.Join(targetContainer.Names, ", ")
	util.Log.Infof("Fetching logs for container %s (%s)...", containerName, containerID[:12])

	logReader, err := docker.GetContainerLogs(ctx, containerID, follow, tail)
	if err != nil {
		return fmt.Errorf("failed to retrieve logs")
	}
	defer func(logReader io.ReadCloser) {
		err := logReader.Close()
		if err != nil {
			util.Log.Errorf("Error closing log reader: %v", err)
		} else {
			util.Log.Debug("Log reader closed successfully.")
		}
	}(logReader)

	_, err = io.Copy(os.Stdout, logReader)
	if err != nil && err != io.EOF {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			util.Log.Debug("Log streaming context cancelled or deadline exceeded.")
			return nil
		}
		util.Log.Errorf("Error streaming logs: %v", err)
		return fmt.Errorf("error streaming logs: %w", err)
	}

	return nil
}

// GetAppLogsAsString fetches logs for the active container and returns as a string.
func GetAppLogsAsString(ctx context.Context, reflowBasePath, projectName, env string, tail string) (string, error) {
	util.Log.Debugf("Attempting to get logs as string for project '%s', environment '%s'...", projectName, env)

	projState, err := config.LoadProjectState(reflowBasePath, projectName)
	if err != nil {
		return "", fmt.Errorf("failed to load project state for '%s': %w", projectName, err)
	}

	// --- Logic to find the targetContainer (similar to StreamAppLogs) ---
	var activeSlot string
	var activeCommit string
	if env == "test" {
		activeSlot = projState.Test.ActiveSlot
		activeCommit = projState.Test.ActiveCommit
	} else if env == "prod" {
		activeSlot = projState.Prod.ActiveSlot
		activeCommit = projState.Prod.ActiveCommit
	} else {
		return "", fmt.Errorf("invalid environment specified: %s", env)
	}

	if activeCommit == "" || activeSlot == "" {
		return "", fmt.Errorf("no active deployment found in state for project '%s', environment '%s'", projectName, env)
	}

	labels := map[string]string{
		docker.LabelProject:     projectName,
		docker.LabelEnvironment: env,
		docker.LabelSlot:        activeSlot,
	}

	containers, err := docker.FindContainersByLabels(ctx, labels)
	if err != nil {
		return "", fmt.Errorf("failed to find containers for project '%s' env '%s' slot '%s': %w", projectName, env, activeSlot, err)
	}

	var targetContainer *container.Summary = nil
	for i := range containers {
		c := containers[i]
		commitMatches := c.Labels[docker.LabelCommit] == activeCommit
		if c.Labels[docker.LabelCommit] == "" {
			commitMatches = true
		}

		if c.State == "running" && commitMatches {
			targetContainer = &c
			break
		}
	}

	if targetContainer == nil {
		var latestExitedContainer *container.Summary = nil
		for i := range containers {
			c := containers[i]
			commitMatches := c.Labels[docker.LabelCommit] == activeCommit
			if c.Labels[docker.LabelCommit] == "" {
				commitMatches = true
			}
			if c.State == "exited" && commitMatches {
				if latestExitedContainer == nil || c.Created > latestExitedContainer.Created {
					latestExitedContainer = &c
				}
			}
		}
		if latestExitedContainer == nil {
			return "", fmt.Errorf("no running or recently stopped container found for project '%s' env '%s' slot '%s' commit '%s'", projectName, env, activeSlot, activeCommit[:7])
		}
		targetContainer = latestExitedContainer
		util.Log.Debugf("Returning logs from last exited container: %s", targetContainer.ID[:12])
	}
	// --- End finding target container ---

	containerID := targetContainer.ID
	util.Log.Debugf("Getting logs as string for container %s (Tail: %s)", containerID[:12], tail)

	logReader, err := docker.GetContainerLogs(ctx, containerID, false, tail)
	if err != nil {
		return "", fmt.Errorf("failed to retrieve logs: %w", err)
	}
	defer logReader.Close()

	var logBuf bytes.Buffer
	_, err = io.Copy(&logBuf, logReader)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("error reading logs to buffer: %w", err)
	}

	return logBuf.String(), nil
}
