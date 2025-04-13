package orchestrator

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/app"
	"reflow/internal/config"
	"reflow/internal/docker"
	internalGit "reflow/internal/git"
	"reflow/internal/nginx"
	"reflow/internal/util"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

const defaultCommit = "HEAD"

// DeployTest orchestrates the deployment process to the 'test' environment.
func DeployTest(ctx context.Context, reflowBasePath, projectName, commitIsh string) (err error) {
	util.Log.Infof("Starting deployment for project '%s' to 'test' environment...", projectName)
	projectBasePath := config.GetProjectBasePath(reflowBasePath, projectName)
	repoPath := filepath.Join(projectBasePath, config.RepoDirName)

	var projCfg *config.ProjectConfig
	var projState *config.ProjectState
	var globalCfg *config.GlobalConfig
	var repo *gogit.Repository
	var resolvedHash *plumbing.Hash
	var commitHash string
	var activeSlot, inactiveSlot string
	var imageTag string
	var dockerfilePath string
	var newContainerID string
	var containerName string

	defer func() {
		if err != nil && newContainerID != "" {
			util.Log.Errorf("Deployment failed: %v", err)
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
		if dockerfilePath != "" {
			_ = os.Remove(dockerfilePath)
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
		util.Log.Warnf("Could not load project state, assuming first deployment: %v", err)
		projState = &config.ProjectState{}
	}

	globalCfg, err = config.LoadGlobalConfig(reflowBasePath)
	if err != nil {
		util.Log.Warnf("Could not load global config: %v", err)
		globalCfg = &config.GlobalConfig{}
	}

	// --- 2. Determine Target Commit ---
	util.Log.Debug("Determining target commit...")
	targetCommitIsh := commitIsh
	if targetCommitIsh == "" {
		targetCommitIsh = defaultCommit
		util.Log.Infof("No commit specified, defaulting to %s", defaultCommit)
	}

	// --- 3. Update & Checkout Repo ---
	util.Log.Info("Updating repository...")
	if err = internalGit.FetchUpdates(repoPath); err != nil {
		return fmt.Errorf("failed to fetch repository updates: %w", err)
	}

	repo, err = gogit.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository at %s: %w", repoPath, err)
	}
	resolvedHash, err = repo.ResolveRevision(plumbing.Revision(targetCommitIsh))
	if err != nil {
		return fmt.Errorf("failed to resolve revision '%s': %w", targetCommitIsh, err)
	}
	commitHash = resolvedHash.String()
	util.Log.Infof("Resolved '%s' to commit: %s", targetCommitIsh, commitHash)

	util.Log.Infof("Checking out commit %s...", commitHash[:7])
	if err = internalGit.CheckoutCommit(repoPath, commitHash); err != nil {
		return fmt.Errorf("failed to checkout commit %s: %w", commitHash, err)
	}

	// --- 4. Identify Slots ---
	util.Log.Debug("Identifying deployment slots...")

	activeSlot = projState.Test.ActiveSlot
	if activeSlot == "blue" {
		inactiveSlot = "green"
	} else {
		inactiveSlot = "blue"
	}

	util.Log.Infof("Targeting inactive slot: %s (Active slot: %s)", inactiveSlot, activeSlot)

	// --- 5. Build Docker Image ---
	imageTag = fmt.Sprintf("%s:%s", strings.ToLower(projectName), commitHash)
	util.Log.Infof("Preparing to build image: %s", imageTag)
	dockerfileData := docker.DockerfileData{
		NodeVersion: projCfg.NodeVersion,
		AppPort:     projCfg.AppPort,
	}
	dockerfileContent, err := docker.GenerateDockerfileContent(dockerfileData)
	if err != nil {
		return fmt.Errorf("failed to generate dockerfile content: %w", err)
	}

	dockerfilePath = filepath.Join(repoPath, ".reflow-dockerfile")
	if err = os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0644); err != nil {
		return fmt.Errorf("failed to write temporary dockerfile: %w", err)
	}

	buildArgs := map[string]*string{"NODE_VERSION": &projCfg.NodeVersion}
	err = docker.BuildImage(ctx, dockerfilePath, repoPath, imageTag, buildArgs)
	if err != nil {
		return fmt.Errorf("docker image build failed: %w", err)
	}
	util.Log.Infof("Image build successful: %s", imageTag)

	// --- 6. Stop/Remove Old Inactive Container ---
	util.Log.Infof("Cleaning up previous inactive slot '%s' container if exists...", inactiveSlot)
	oldLabels := map[string]string{
		docker.LabelProject:     projectName,
		docker.LabelEnvironment: "test",
		docker.LabelSlot:        inactiveSlot,
	}

	oldContainers, findErr := docker.FindContainersByLabels(ctx, oldLabels)
	if findErr != nil {
		return fmt.Errorf("failed to check for old inactive containers: %w", findErr)
	}
	for _, oldC := range oldContainers {
		util.Log.Warnf("Found old container %s (%s) in inactive slot. Stopping and removing.", oldC.ID[:12], strings.Join(oldC.Names, ","))
		_ = docker.StopContainer(ctx, oldC.ID, nil)
		if rmErr := docker.RemoveContainer(ctx, oldC.ID); rmErr != nil {
			util.Log.Errorf("Failed to remove old container %s: %v", oldC.ID[:12], rmErr)
		}
	}

	// --- 7. Start New Container ---
	containerName = fmt.Sprintf("%s-test-%s-%s", strings.ToLower(projectName), inactiveSlot, commitHash[:7])
	util.Log.Infof("Starting new container '%s' for slot '%s'...", containerName, inactiveSlot)
	envFilePath := ""
	if projCfg.Environments["test"].EnvFile != "" {
		envFilePath = filepath.Join(repoPath, projCfg.Environments["test"].EnvFile)
	}

	envVars, err := loadEnvFile(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to load environment variables: %w", err)
	}

	envVars = append(envVars, fmt.Sprintf("PORT=%d", projCfg.AppPort))
	newLabels := map[string]string{
		docker.LabelManaged:     "true",
		docker.LabelProject:     projectName,
		docker.LabelEnvironment: "test",
		docker.LabelSlot:        inactiveSlot,
		docker.LabelCommit:      commitHash,
	}

	runOptions := docker.ContainerRunOptions{
		ImageName:     imageTag,
		ContainerName: containerName,
		NetworkName:   config.ReflowNetworkName,
		Labels:        newLabels,
		EnvVars:       envVars,
		AppPort:       projCfg.AppPort,
		RestartPolicy: "unless-stopped",
	}

	newContainerID, err = docker.RunContainer(ctx, runOptions)
	if err != nil {
		return fmt.Errorf("failed to run new container: %w", err)
	}
	util.Log.Infof("New container started: %s (ID: %s)", containerName, newContainerID[:12])

	// --- 8. Health Check ---
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
			util.Log.Infof("Container '%s' passed health check after %v.", containerName, time.Since(healthCheckStartTime))
			break
		} else {
			util.Log.Debugf("Container '%s' not healthy yet, retrying in %v...", containerName, healthInterval)
		}

		select {
		case <-time.After(healthInterval):
		case <-ctx.Done():
			return fmt.Errorf("health check cancelled while waiting for interval: %w", ctx.Err())
		}
	}

	if !isHealthy {
		err = fmt.Errorf("container '%s' failed health check: timed out after %v", containerName, healthTimeout)
		return err
	}

	// --- 9. Update Nginx ---
	util.Log.Info("Updating Nginx configuration...")
	domain, err := config.GetEffectiveDomain(globalCfg, projCfg, "test")
	if err != nil {
		return fmt.Errorf("failed to determine domain for nginx config: %w", err)
	}
	nginxData := nginx.TemplateData{ProjectName: projectName, Env: "test", Slot: inactiveSlot, ContainerName: containerName, Domain: domain, AppPort: projCfg.AppPort}
	nginxConfContent, err := nginx.GenerateNginxConfig(nginxData)
	if err != nil {
		return fmt.Errorf("failed to generate nginx config: %w", err)
	}
	err = nginx.WriteNginxConfig(reflowBasePath, projectName, "test", nginxConfContent)
	if err != nil {
		return fmt.Errorf("failed to write nginx config: %w", err)
	}
	if err = nginx.ReloadNginx(ctx); err != nil {
		return fmt.Errorf("failed to reload nginx: %w", err)
	}
	util.Log.Info("Nginx reloaded, traffic switched to new container.")

	// --- 10. Update State ---
	util.Log.Info("Updating deployment state...")
	projState.Test.ActiveSlot = inactiveSlot
	projState.Test.ActiveCommit = commitHash
	projState.Test.PendingCommit = ""
	if inactiveSlot == "blue" {
		projState.Test.InactiveSlot = "green"
	} else {
		projState.Test.InactiveSlot = "blue"
	}

	if err = config.SaveProjectState(reflowBasePath, projectName, projState); err != nil {
		return fmt.Errorf("CRITICAL: Deployment successful, but failed to save updated state: %w", err)
	}

	util.Log.Info("-----------------------------------------------------")
	util.Log.Infof("✅ Deployment to 'test' environment for project '%s' successful!", projectName)
	util.Log.Infof("   Commit:  %s (%s)", commitHash, commitHash[:7])
	util.Log.Infof("   Slot:    %s", inactiveSlot)

	domain, domainErr := config.GetEffectiveDomain(globalCfg, projCfg, "test")
	if domainErr == nil {
		accessURL := fmt.Sprintf("%s", domain)
		util.Log.Infof("   URL:     %s (Ensure DNS points to server IP!)", accessURL)
	} else {
		util.Log.Warnf("   URL:     Could not determine URL: %v", domainErr)
	}

	util.Log.Info(" ")
	util.Log.Info("Next steps:")
	util.Log.Infof("  - Check status:  ./t project status %s", projectName)
	util.Log.Infof("  - View logs:     ./t project logs %s --env test -f", projectName)
	util.Log.Infof("  - Approve (Prod):./t approve %s", projectName)
	util.Log.Info("-----------------------------------------------------")

	return nil
}

// loadEnvFile loads environment variables from a specified file.
func loadEnvFile(filePath string) ([]string, error) {
	var vars []string
	if filePath == "" {
		util.Log.Debug("No env file path specified.")
		return vars, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			util.Log.Warnf("Environment file not found at %s, continuing without it.", filePath)
			return vars, nil
		}
		return nil, fmt.Errorf("failed to open env file %s: %w", filePath, err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			util.Log.Errorf("Error closing env file %s: %v", filePath, err)
		} else {
			util.Log.Debugf("Closed env file %s successfully.", filePath)
		}
	}(file)

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			util.Log.Warnf("Skipping invalid line %d in env file %s: Missing '='", lineNumber, filePath)
			continue
		}
		vars = append(vars, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env file %s: %w", filePath, err)
	}
	util.Log.Debugf("Loaded %d variables from %s", len(vars), filePath)
	return vars, nil
}
