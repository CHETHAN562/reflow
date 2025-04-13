package orchestrator

import (
	"context"
	"fmt"
	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/util"
	"strings"

	"github.com/docker/docker/api/types/image"
)

// CleanupProjectEnv cleans up inactive containers for a given project and environment.
func CleanupProjectEnv(ctx context.Context, reflowBasePath, projectName, env string) (cleanedCount int, err error) {
	util.Log.Infof("Starting cleanup for project '%s', environment '%s'...", projectName, env)
	cleanedCount = 0

	projState, err := config.LoadProjectState(reflowBasePath, projectName)
	if err != nil {
		return 0, fmt.Errorf("failed to load project state for '%s': %w", projectName, err)
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
		return 0, fmt.Errorf("invalid environment specified: %s", env)
	}

	if activeCommit == "" || activeSlot == "" {
		util.Log.Infof("No active deployment found in state for project '%s', environment '%s'. Skipping container cleanup.", projectName, env)
		return 0, nil
	}

	util.Log.Debugf("Active state for %s/%s: Slot=%s, Commit=%s", projectName, env, activeSlot, activeCommit[:7])

	// Find ALL containers managed by Reflow for this project and environment
	baseLabels := map[string]string{
		docker.LabelProject:     projectName,
		docker.LabelEnvironment: env,
	}

	allContainers, err := docker.FindContainersByLabels(ctx, baseLabels)
	if err != nil {
		return 0, fmt.Errorf("failed to find containers for project '%s' env '%s': %w", projectName, env, err)
	}

	if len(allContainers) == 0 {
		util.Log.Infof("No containers found for project '%s', environment '%s'. Cleanup complete.", projectName, env)
		return 0, nil
	}

	util.Log.Infof("Found %d container(s) for %s/%s. Checking for inactive ones...", len(allContainers), projectName, env)

	var cleanupErrors []string
	for _, c := range allContainers {
		containerName := strings.Join(c.Names, ", ")
		containerID := c.ID[:12]
		slotLabel := c.Labels[docker.LabelSlot]
		commitLabel := c.Labels[docker.LabelCommit]

		isInactive := slotLabel != activeSlot || commitLabel != activeCommit

		if isInactive {
			util.Log.Warnf("Found inactive container: %s (ID: %s, Slot: %s, Commit: %s). Stopping and removing.",
				containerName, containerID, slotLabel, commitLabel[:7])

			stopErr := docker.StopContainer(ctx, c.ID, nil)
			if stopErr != nil {
				util.Log.Debugf("Ignoring error stopping potentially already stopped container %s: %v", containerID, stopErr)
			}

			removeErr := docker.RemoveContainer(ctx, c.ID)
			if removeErr != nil {
				errMsg := fmt.Sprintf("failed to remove inactive container %s (%s): %v", containerName, containerID, removeErr)
				util.Log.Errorf(errMsg)
				cleanupErrors = append(cleanupErrors, errMsg)
			} else {
				util.Log.Infof("Removed inactive container %s (%s)", containerName, containerID)
				cleanedCount++
			}
		} else {
			util.Log.Debugf("Skipping active container: %s (ID: %s, Slot: %s, Commit: %s)", containerName, containerID, slotLabel, commitLabel[:7])
		}
	}

	util.Log.Infof("Container cleanup complete for project '%s', environment '%s'. Removed %d inactive container(s).", projectName, env, cleanedCount)

	if len(cleanupErrors) > 0 {
		return cleanedCount, fmt.Errorf("encountered errors during container cleanup:\n - %s", strings.Join(cleanupErrors, "\n - "))
	}

	return cleanedCount, nil
}

// PruneProjectImages removes Docker images associated with inactive commits for a project.
func PruneProjectImages(ctx context.Context, reflowBasePath, projectName string) (prunedCount int, err error) {
	util.Log.Warn("--- Starting Image Pruning ---")
	util.Log.Warn("This will remove Docker images tagged for this project that do not match")
	util.Log.Warn("the currently active commit in EITHER the 'test' OR 'prod' environment.")
	util.Log.Warn("Ensure you want to remove these images, as it might affect rollbacks.")
	prunedCount = 0

	projState, err := config.LoadProjectState(reflowBasePath, projectName)
	if err != nil {
		return 0, fmt.Errorf("failed to load project state for '%s' during image prune: %w", projectName, err)
	}

	activeCommits := make(map[string]bool)
	if projState.Test.ActiveCommit != "" {
		activeCommits[projState.Test.ActiveCommit] = true
	}
	if projState.Prod.ActiveCommit != "" {
		activeCommits[projState.Prod.ActiveCommit] = true
	}

	if len(activeCommits) == 0 {
		util.Log.Info("No active deployments found for project '%s'. Skipping image prune.", projectName)
		return 0, nil
	}
	util.Log.Debugf("Active commits for project '%s': %v", projectName, activeCommits)

	cli, err := docker.GetClient()
	if err != nil {
		return 0, err
	}

	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to list images for pruning: %w", err)
	}

	util.Log.Infof("Found %d total images. Checking for prunable images for project '%s'...", len(images), projectName)

	var pruneErrors []string
	imagePrefix := strings.ToLower(projectName) + ":"

	for _, img := range images {
		isProjectImage := false
		commitHash := ""
		repoTags := img.RepoTags

		if repoTags == nil {
			continue
		}

		for _, tag := range repoTags {
			if strings.HasPrefix(tag, imagePrefix) {
				isProjectImage = true
				commitHash = strings.TrimPrefix(tag, imagePrefix)
				break
			}
		}

		if !isProjectImage || commitHash == "" {
			continue
		}

		if _, isActive := activeCommits[commitHash]; !isActive {
			util.Log.Warnf("Found prunable image: %s (ID: %s, Commit: %s)", repoTags, img.ID[:12], commitHash[:7])

			err := docker.RemoveImage(ctx, img.ID)
			if err != nil {
				errMsg := fmt.Sprintf("failed to prune image %s (%s): %v", img.ID[:12], repoTags, err)
				util.Log.Errorf(errMsg)
				pruneErrors = append(pruneErrors, errMsg)
			} else {
				util.Log.Infof("Pruned image %s (%s)", img.ID[:12], repoTags)
				prunedCount++
			}
		} else {
			util.Log.Debugf("Skipping active image: %s (Commit: %s)", repoTags, commitHash[:7])
		}
	}

	util.Log.Infof("Image pruning complete for project '%s'. Removed %d image(s).", projectName, prunedCount)
	util.Log.Warn("--- Finished Image Pruning ---")

	if len(pruneErrors) > 0 {
		return prunedCount, fmt.Errorf("encountered errors during image pruning:\n - %s", strings.Join(pruneErrors, "\n - "))
	}

	return prunedCount, nil
}
