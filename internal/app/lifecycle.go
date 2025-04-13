package app

import (
	"bytes"
	"context"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"io"
	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/util"
	"strings"
	"time"
)

// StopProjectEnv stops the active container(s) for a specific project environment.
func StopProjectEnv(ctx context.Context, reflowBasePath, projectName, env string) error {
	util.Log.Infof("Attempting to stop active container for project '%s', environment '%s'...", projectName, env)

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
		util.Log.Infof("No active deployment found in state for project '%s', environment '%s'. Nothing to stop.", projectName, env)
		return nil
	}

	labels := map[string]string{
		docker.LabelProject:     projectName,
		docker.LabelEnvironment: env,
		docker.LabelSlot:        activeSlot,
		// docker.LabelCommit: activeCommit,
	}

	containers, err := docker.FindContainersByLabels(ctx, labels)
	if err != nil {
		return fmt.Errorf("failed to find containers for project '%s' env '%s' slot '%s': %w", projectName, env, activeSlot, err)
	}

	if len(containers) == 0 {
		util.Log.Warnf("State indicates active deployment for '%s'/'%s' slot '%s', but no matching container found.", projectName, env, activeSlot)
		return nil
	}

	stoppedCount := 0
	for _, c := range containers {
		containerName := strings.Join(c.Names, ", ")
		containerID := c.ID[:12]
		util.Log.Debugf("Found active container: %s (ID: %s)", containerName, containerID)

		err := docker.StopContainer(ctx, c.ID, nil)
		if err != nil {
			util.Log.Errorf("Failed to stop container %s (%s): %v", containerName, containerID, err)
		} else {
			util.Log.Infof("Successfully stopped container %s (%s)", containerName, containerID)
			stoppedCount++
		}
	}

	if stoppedCount == 0 && len(containers) > 0 {
		return fmt.Errorf("attempted to stop %d container(s), but failed for all", len(containers))
	}

	util.Log.Infof("Stop operation complete for project '%s', environment '%s'. Stopped %d container(s).", projectName, env, stoppedCount)
	return nil
}

// StartProjectEnv starts the previously stopped active container(s) for a specific project environment.
func StartProjectEnv(ctx context.Context, reflowBasePath, projectName, env string) error {
	util.Log.Infof("Attempting to start stopped container for project '%s', environment '%s'...", projectName, env)

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
		util.Log.Infof("No active deployment found in state for project '%s', environment '%s'. Nothing to start.", projectName, env)
		return nil
	}

	labels := map[string]string{
		docker.LabelProject:     projectName,
		docker.LabelEnvironment: env,
		docker.LabelSlot:        activeSlot,
		// docker.LabelCommit: activeCommit,
	}

	containers, err := docker.FindContainersByLabels(ctx, labels)
	if err != nil {
		return fmt.Errorf("failed to find containers for project '%s' env '%s' slot '%s': %w", projectName, env, activeSlot, err)
	}

	if len(containers) == 0 {
		util.Log.Warnf("State indicates active deployment for '%s'/'%s' slot '%s', but no matching container found (running or stopped).", projectName, env, activeSlot)
		return nil
	}

	startedCount := 0
	for _, c := range containers {
		containerName := strings.Join(c.Names, ", ")
		containerID := c.ID[:12]
		util.Log.Debugf("Found container corresponding to active state: %s (ID: %s, State: %s)", containerName, containerID, c.State)

		if c.State == "running" {
			util.Log.Infof("Container %s (%s) is already running.", containerName, containerID)
			startedCount++
			continue
		}

		err := docker.StartContainer(ctx, c.ID)
		if err != nil {
			util.Log.Errorf("Failed to start container %s (%s): %v", containerName, containerID, err)
		} else {
			util.Log.Infof("Successfully started container %s (%s)", containerName, containerID)
			startedCount++
		}
	}

	if startedCount == 0 && len(containers) > 0 {
		return fmt.Errorf("attempted to start %d container(s), but failed for all", len(containers))
	}

	util.Log.Infof("Start operation complete for project '%s', environment '%s'. Started/Verified %d container(s).", projectName, env, startedCount)
	return nil
}

// CheckTcpHealthFromNginx performs a single TCP port check from within the reflow-nginx container.
// Returns true if the connection was successful (nc exit code 0), false otherwise.
func CheckTcpHealthFromNginx(ctx context.Context, targetContainerName string, appPort int) (bool, error) {
	cli, err := docker.GetClient()
	if err != nil {
		return false, err
	}

	nginxContainerName := config.ReflowNginxContainerName
	targetHostPort := fmt.Sprintf("%d", appPort)
	cmd := []string{"nc", "-z", "-w", "2", targetContainerName, targetHostPort}

	util.Log.Debugf("Executing health check inside '%s': %s", nginxContainerName, strings.Join(cmd, " "))

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execIDResp, err := cli.ContainerExecCreate(ctx, nginxContainerName, execConfig)
	if err != nil {
		if strings.Contains(err.Error(), "No such container") {
			util.Log.Errorf("Cannot run health check: Nginx container '%s' not found. Was 'reflow init' successful?", nginxContainerName)
			return false, fmt.Errorf("nginx container '%s' not found", nginxContainerName)
		}
		util.Log.Errorf("Failed to create docker exec for health check: %v", err)
		return false, fmt.Errorf("failed to create health check exec: %w", err)
	}

	execAttachResp, err := cli.ContainerExecAttach(ctx, execIDResp.ID, container.ExecAttachOptions{})
	if err != nil {
		util.Log.Errorf("Failed to attach to health check exec: %v", err)
		return false, fmt.Errorf("failed to attach to health check exec: %w", err)
	}
	defer execAttachResp.Close()

	var outputBuffer bytes.Buffer
	_, err = io.Copy(&outputBuffer, execAttachResp.Reader)
	outputStr := outputBuffer.String()
	if err != nil && err != io.EOF {
		util.Log.Warnf("Error reading health check exec output: %v", err)
	}
	if outputStr != "" {
		util.Log.Debugf("Health check exec output: %s", strings.TrimSpace(outputStr))
	}

	var exitCode = -1
	inspectTimeout := time.After(5 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for exitCode == -1 {
		select {
		case <-ticker.C:
			execInspectResp, inspectErr := cli.ContainerExecInspect(ctx, execIDResp.ID)
			if inspectErr != nil {
				util.Log.Debugf("Error inspecting health check exec (will retry): %v", inspectErr)
			} else {
				if execInspectResp.Running {
					util.Log.Debugf("Health check exec still running...")
				} else {
					exitCode = execInspectResp.ExitCode
					util.Log.Debugf("Health check exec finished with exit code: %d", exitCode)
				}
			}
		case <-inspectTimeout:
			util.Log.Errorf("Timeout waiting for health check exec to complete inspection.")
			return false, fmt.Errorf("timeout inspecting health check exec")
		case <-ctx.Done():
			return false, fmt.Errorf("health check context cancelled during inspection: %w", ctx.Err())
		}
	}

	// nc -z returns 0 on success, non-zero on failure
	return exitCode == 0, nil
}
