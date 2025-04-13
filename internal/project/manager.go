package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/util"
)

// Summary ProjectSummary holds summarized information for the 'list' command.
type Summary struct {
	Name       string
	RepoURL    string
	TestStatus string // e.g., "Commit: abc1234" or "Not Deployed"
	ProdStatus string // e.g., "Commit: def5678" or "Not Deployed"
}

// EnvironmentDetails holds detailed status for one environment (test/prod).
type EnvironmentDetails struct {
	EnvironmentName string
	IsActive        bool
	ActiveCommit    string
	ActiveSlot      string
	EffectiveDomain string
	EnvFilePath     string
	AppPort         int
	ContainerStatus string
	ContainerID     string
	ContainerNames  []string
}

// Details ProjectDetails holds comprehensive information for the 'status' command.
type Details struct {
	Name           string
	RepoURL        string
	ConfigFilePath string
	StateFilePath  string
	LocalRepoPath  string
	TestDetails    EnvironmentDetails
	ProdDetails    EnvironmentDetails
}

// ListProjects scans the apps directory and returns a summary for each valid project.
func ListProjects(reflowBasePath string) ([]Summary, error) {
	appsPath := filepath.Join(reflowBasePath, config.AppsDirName)
	util.Log.Debugf("Scanning for projects in: %s", appsPath)

	var summaries []Summary

	entries, err := os.ReadDir(appsPath)
	if err != nil {
		if os.IsNotExist(err) {
			util.Log.Debugf("Apps directory '%s' does not exist, returning empty project list.", appsPath)
			return summaries, nil
		}
		util.Log.Errorf("Failed to read apps directory '%s': %v", appsPath, err)
		return nil, fmt.Errorf("failed to read apps directory %s: %w", appsPath, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			util.Log.Debugf("Skipping non-directory entry in apps folder: %s", entry.Name())
			continue
		}

		projectName := entry.Name()
		util.Log.Debugf("Processing potential project: %s", projectName)

		projCfg, err := config.LoadProjectConfig(reflowBasePath, projectName)
		if err != nil {
			util.Log.Warnf("Skipping project '%s': Failed to load config: %v", projectName, err)
			continue
		}

		projState, err := config.LoadProjectState(reflowBasePath, projectName)
		if err != nil {
			util.Log.Warnf("Could not load state for project '%s', assuming not deployed: %v", projectName, err)
			projState = &config.ProjectState{}
		}

		summary := Summary{
			Name:    projCfg.ProjectName,
			RepoURL: projCfg.GithubRepo,
		}

		if projState.Test.ActiveCommit != "" {
			summary.TestStatus = fmt.Sprintf("Commit: %s (%s)", projState.Test.ActiveCommit[:7], projState.Test.ActiveSlot)
		} else {
			summary.TestStatus = "Not Deployed"
		}

		if projState.Prod.ActiveCommit != "" {
			summary.ProdStatus = fmt.Sprintf("Commit: %s (%s)", projState.Prod.ActiveCommit[:7], projState.Prod.ActiveSlot)
		} else {
			summary.ProdStatus = "Not Deployed"
		}

		summaries = append(summaries, summary)
	}

	util.Log.Debugf("Found %d valid projects.", len(summaries))
	return summaries, nil
}

// GetProjectDetails gathers detailed information about a specific project.
func GetProjectDetails(ctx context.Context, reflowBasePath, projectName string) (*Details, error) {
	util.Log.Debugf("Getting details for project: %s", projectName)
	projectBasePath := config.GetProjectBasePath(reflowBasePath, projectName)

	// --- Load Configs ---
	projCfg, err := config.LoadProjectConfig(reflowBasePath, projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to load project config for '%s': %w", projectName, err)
	}

	projState, err := config.LoadProjectState(reflowBasePath, projectName)
	if err != nil {
		util.Log.Warnf("Could not load state for project '%s', assuming not deployed: %v", projectName, err)
		projState = &config.ProjectState{}
	}

	globalCfg, err := config.LoadGlobalConfig(reflowBasePath)
	if err != nil {
		util.Log.Warnf("Could not load global config while getting project details: %v. Domain calculation might be incomplete.", err)
		globalCfg = &config.GlobalConfig{}
	}

	details := &Details{
		Name:           projCfg.ProjectName,
		RepoURL:        projCfg.GithubRepo,
		ConfigFilePath: filepath.Join(projectBasePath, config.ProjectConfigFileName),
		StateFilePath:  filepath.Join(projectBasePath, config.ProjectStateFileName),
		LocalRepoPath:  filepath.Join(projectBasePath, config.RepoDirName),
		TestDetails:    EnvironmentDetails{EnvironmentName: "test"},
		ProdDetails:    EnvironmentDetails{EnvironmentName: "prod"},
	}

	// --- Populate Environment Details ---
	populateEnvDetails(ctx, projCfg, projState.Test, &details.TestDetails, globalCfg)
	populateEnvDetails(ctx, projCfg, projState.Prod, &details.ProdDetails, globalCfg)

	return details, nil
}

// populateEnvDetails helper function to fill details for test or prod env.
func populateEnvDetails(ctx context.Context, projCfg *config.ProjectConfig, envState config.EnvironmentState, details *EnvironmentDetails, globalCfg *config.GlobalConfig) {
	envName := details.EnvironmentName

	envCfg, ok := projCfg.Environments[envName]
	if !ok {
		util.Log.Warnf("Environment '%s' not found in project config for %s", envName, projCfg.ProjectName)
		details.ContainerStatus = "Config Error"
		return
	}
	details.EnvFilePath = envCfg.EnvFile
	details.AppPort = projCfg.AppPort

	details.IsActive = envState.ActiveCommit != ""
	details.ActiveSlot = envState.ActiveSlot
	if details.IsActive {
		details.ActiveCommit = envState.ActiveCommit[:7]
	} else {
		details.ContainerStatus = "Not Deployed"
		details.ActiveSlot = "N/A"
		details.ActiveCommit = "N/A"
		domain, err := config.GetEffectiveDomain(globalCfg, projCfg, envName)
		if err == nil {
			details.EffectiveDomain = domain
		} else {
			details.EffectiveDomain = fmt.Sprintf("Error: %v", err)
		}
		return
	}

	domain, err := config.GetEffectiveDomain(globalCfg, projCfg, envName)
	if err == nil {
		details.EffectiveDomain = domain
	} else {
		details.EffectiveDomain = fmt.Sprintf("Error: %v", err)
	}

	// --- Check Docker Container Status ---
	labels := map[string]string{
		docker.LabelProject:     projCfg.ProjectName,
		docker.LabelEnvironment: envName,
		docker.LabelSlot:        envState.ActiveSlot,
		// docker.LabelCommit: envState.ActiveCommit,
	}

	foundContainers, err := docker.FindContainersByLabels(ctx, labels)
	if err != nil {
		details.ContainerStatus = fmt.Sprintf("Error querying Docker: %v", err)
		return
	}

	if len(foundContainers) == 0 {
		details.ContainerStatus = "Not Found (Expected based on state!)"
	} else if len(foundContainers) > 1 {
		details.ContainerStatus = "Multiple Found (!)"
		details.ContainerID = "Multiple"
		for _, c := range foundContainers {
			details.ContainerNames = append(details.ContainerNames, c.Names...)
		}
		util.Log.Warnf("Found multiple containers matching labels for %s/%s/%s: %v", projCfg.ProjectName, envName, envState.ActiveSlot, details.ContainerNames)
	} else {
		container := foundContainers[0]
		details.ContainerStatus = docker.GetContainerStatusString(container)
		details.ContainerID = container.ID[:12]
		details.ContainerNames = container.Names
	}
}
