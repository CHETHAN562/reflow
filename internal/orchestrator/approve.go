package orchestrator

import (
	"context"
	"fmt"
	"path/filepath"
	"reflow/internal/app"
	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/nginx"
	"reflow/internal/util"
	"strings"
	"time"
)

// ApproveProd promotes a project from 'test' to 'prod' environment.
func ApproveProd(ctx context.Context, reflowBasePath, projectName string) (err error) {
	util.Log.Infof("Starting approval process for project '%s' to 'prod' environment...", projectName)
	projectBasePath := config.GetProjectBasePath(reflowBasePath, projectName)
	repoPath := filepath.Join(projectBasePath, config.RepoDirName)

	var projCfg *config.ProjectConfig
	var projState *config.ProjectState
	var globalCfg *config.GlobalConfig
	var approvedCommitHash string
	var imageTag string
	var prodActiveSlot, prodInactiveSlot string
	var newContainerID string
	var containerName string

	defer func() {
		if err != nil && newContainerID != "" {
			util.Log.Errorf("Approval failed: %v", err)
			util.Log.Warnf("Attempting simple rollback: stopping and removing newly started container %s...", newContainerID[:12])
			cleanupCtx := context.Background()
			_ = docker.StopContainer(cleanupCtx, newContainerID, nil)
			rmErr := docker.RemoveContainer(cleanupCtx, newContainerID)
			if rmErr != nil {
				util.Log.Errorf("Rollback cleanup failed: Could not remove container %s: %v", newContainerID[:12], rmErr)
			} else {
				util.Log.Infof("Rollback cleanup: Removed container %s", newContainerID[:12])
			}
		}
	}()

	// --- 1. Load Configs ---
	util.Log.Debug("Loading configurations...")
	projCfg, err = config.LoadProjectConfig(reflowBasePath, projectName)
	if err != nil {
		return fmt.Errorf("failed to load project config: %w", err)
	}
	projState, err = config.LoadProjectState(reflowBasePath, projectName)
	if err != nil {
		return fmt.Errorf("failed to load project state: %w", err)
	}
	globalCfg, err = config.LoadGlobalConfig(reflowBasePath)
	if err != nil {
		util.Log.Warnf("Could not load global config: %v", err)
		globalCfg = &config.GlobalConfig{}
	}

	// --- 2. Check Test State ---
	util.Log.Debug("Checking 'test' environment status...")
	if projState.Test.ActiveCommit == "" || projState.Test.ActiveSlot == "" {
		return fmt.Errorf("no active deployment found in 'test' environment for project '%s' to approve", projectName)
	}

	approvedCommitHash = projState.Test.ActiveCommit
	util.Log.Infof("Approving commit %s currently active in 'test' (slot: %s)", approvedCommitHash[:7], projState.Test.ActiveSlot)

	// --- 3. Identify Prod Slots ---
	util.Log.Debug("Identifying prod deployment slots...")

	prodActiveSlot = projState.Prod.ActiveSlot
	if prodActiveSlot == "blue" {
		prodInactiveSlot = "green"
	} else {
		prodInactiveSlot = "blue"
	}

	util.Log.Infof("Targeting prod inactive slot: %s (Active slot: %s)", prodInactiveSlot, prodActiveSlot)

	// --- 4. Find Docker Image ---
	imageTag = fmt.Sprintf("%s:%s", strings.ToLower(projectName), approvedCommitHash)
	util.Log.Infof("Verifying required image exists: %s", imageTag)

	existingImage, err := docker.FindImage(ctx, imageTag)
	if err != nil {
		return fmt.Errorf("error checking for image %s: %w", imageTag, err)
	}

	if existingImage == nil {
		return fmt.Errorf("approved image %s not found locally. Was the 'test' deployment successful", imageTag)
	}
	util.Log.Debugf("Found approved image %s (ID: %s)", imageTag, existingImage.ID)

	// --- 5. Stop/Remove Old Inactive Prod Container ---
	util.Log.Infof("Cleaning up previous prod inactive slot '%s' container if exists...", prodInactiveSlot)
	oldProdLabels := map[string]string{docker.LabelProject: projectName, docker.LabelEnvironment: "prod", docker.LabelSlot: prodInactiveSlot}
	oldProdContainers, findErr := docker.FindContainersByLabels(ctx, oldProdLabels)
	if findErr != nil {
		return fmt.Errorf("failed to check for old inactive prod containers: %w", findErr)
	}
	for _, oldC := range oldProdContainers { /* ... remove logic ... */
		_ = docker.StopContainer(ctx, oldC.ID, nil)
		if rmErr := docker.RemoveContainer(ctx, oldC.ID); rmErr != nil {
			util.Log.Errorf("Failed to remove old prod container %s: %v", oldC.ID[:12], rmErr)
		}
	}

	// --- 6. Start New Prod Container ---
	containerName = fmt.Sprintf("%s-prod-%s-%s", strings.ToLower(projectName), prodInactiveSlot, approvedCommitHash[:7])
	util.Log.Infof("Starting new prod container '%s' for slot '%s'...", containerName, prodInactiveSlot)
	envFilePath := ""
	if projCfg.Environments["prod"].EnvFile != "" {
		envFilePath = filepath.Join(repoPath, projCfg.Environments["prod"].EnvFile)
	}

	util.Log.Debugf("Loading environment variables from file: %s", envFilePath)
	envVars, err := loadEnvFile(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to load prod environment variables: %w", err)
	}

	envVars = append(envVars, fmt.Sprintf("PORT=%d", projCfg.AppPort))
	newProdLabels := map[string]string{
		docker.LabelManaged:     "true",
		docker.LabelProject:     projectName,
		docker.LabelEnvironment: "prod",
		docker.LabelSlot:        prodInactiveSlot,
		docker.LabelCommit:      approvedCommitHash,
	}

	runOptions := docker.ContainerRunOptions{
		ImageName:     imageTag,
		ContainerName: containerName,
		NetworkName:   config.ReflowNetworkName,
		Labels:        newProdLabels,
		EnvVars:       envVars,
		AppPort:       projCfg.AppPort,
		RestartPolicy: "unless-stopped",
	}

	newContainerID, err = docker.RunContainer(ctx, runOptions)
	if err != nil {
		return fmt.Errorf("failed to run new prod container: %w", err)
	}

	util.Log.Infof("New prod container started: %s (ID: %s)", containerName, newContainerID[:12])

	// --- 7. Health Check ---
	healthTimeout := 60 * time.Second
	healthInterval := 5 * time.Second
	healthCheckStartTime := time.Now()
	isHealthy := false

	util.Log.Infof("Performing health check via TCP connection from Nginx container (timeout %v)...", healthTimeout)

	for time.Since(healthCheckStartTime) < healthTimeout {
		select {
		case <-ctx.Done():
			return fmt.Errorf("health check cancelled: %w", ctx.Err())
		default:
		}

		util.Log.Debugf("Polling health for %s...", containerName)
		healthy, checkErr := app.CheckTcpHealthFromNginx(ctx, containerName, projCfg.AppPort)

		if checkErr != nil {
			util.Log.Warnf("Health check poll failed for %s: %v", containerName, checkErr)
		} else if healthy {
			isHealthy = true
			util.Log.Infof("Prod container '%s' passed health check after %v.", containerName, time.Since(healthCheckStartTime))
			break
		} else {
			util.Log.Debugf("Prod container '%s' not healthy yet, retrying in %v...", containerName, healthInterval)
		}

		select {
		case <-time.After(healthInterval):
		case <-ctx.Done():
			return fmt.Errorf("health check cancelled while waiting for interval: %w", ctx.Err())
		}
	}

	if !isHealthy {
		err = fmt.Errorf("prod container '%s' failed health check: timed out after %v", containerName, healthTimeout)
		return err
	}

	// --- 8. Update Nginx for Prod ---
	util.Log.Info("Updating Nginx configuration for prod environment...")
	prodDomain, err := config.GetEffectiveDomain(globalCfg, projCfg, "prod")
	if err != nil {
		return fmt.Errorf("failed to determine prod domain for nginx config: %w", err)
	}
	nginxData := nginx.TemplateData{ProjectName: projectName, Env: "prod", Slot: prodInactiveSlot, ContainerName: containerName, Domain: prodDomain, AppPort: projCfg.AppPort} // Uses projCfg correctly
	nginxConfContent, err := nginx.GenerateNginxConfig(nginxData)
	if err != nil {
		return fmt.Errorf("failed to generate prod nginx config: %w", err)
	}
	err = nginx.WriteNginxConfig(reflowBasePath, projectName, "prod", nginxConfContent)
	if err != nil {
		return fmt.Errorf("failed to write prod nginx config: %w", err)
	}
	if err = nginx.ReloadNginx(ctx); err != nil {
		return fmt.Errorf("failed to reload nginx for prod deployment: %w", err)
	}
	util.Log.Info("Nginx reloaded, prod traffic switched to new container.")

	// --- 9. Update State for Prod ---
	util.Log.Info("Updating deployment state for prod...")
	projState.Prod.ActiveSlot = prodInactiveSlot
	projState.Prod.ActiveCommit = approvedCommitHash
	projState.Prod.PendingCommit = ""
	if prodInactiveSlot == "blue" {
		projState.Prod.InactiveSlot = "green"
	} else {
		projState.Prod.InactiveSlot = "blue"
	}
	if err = config.SaveProjectState(reflowBasePath, projectName, projState); err != nil {
		return fmt.Errorf("CRITICAL: Promotion successful, but failed to save updated prod state: %w", err)
	}

	util.Log.Info("-----------------------------------------------------")
	util.Log.Infof("✅ Promotion of project '%s' to 'prod' environment successful!", projectName)
	util.Log.Infof("   Commit:  %s (%s)", approvedCommitHash, approvedCommitHash[:7])
	util.Log.Infof("   Slot:    %s", prodInactiveSlot)

	prodDomain, domainErr := config.GetEffectiveDomain(globalCfg, projCfg, "prod")
	if domainErr == nil {
		accessURL := fmt.Sprintf("%s", prodDomain)
		util.Log.Infof("   URL:     %s (Ensure DNS points to server IP!)", accessURL)
	} else {
		util.Log.Warnf("   URL:     Could not determine URL: %v", domainErr)
	}

	util.Log.Info(" ")
	util.Log.Info("Next steps:")
	util.Log.Infof("  - Check status:  ./t project status %s", projectName)
	util.Log.Infof("  - View logs:     ./t project logs %s --env prod -f", projectName)
	util.Log.Info("-----------------------------------------------------")

	return nil
}
