package project

import (
	"context"
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"os"
	"path/filepath"
	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/git"
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

// CreateProject handles the core logic of creating a new project.
func CreateProject(reflowBasePath string, args config.CreateProjectArgs) error {
	if args.ProjectName == "" || args.RepoURL == "" {
		return errors.New("project name and repository URL are required")
	}

	util.Log.Infof("Creating new project '%s' from repo '%s'", args.ProjectName, args.RepoURL)

	projectBasePath := config.GetProjectBasePath(reflowBasePath, args.ProjectName)
	repoDestPath := filepath.Join(projectBasePath, config.RepoDirName)

	// --- 1. Check if Project Already Exists ---
	if _, err := os.Stat(projectBasePath); err == nil {
		return fmt.Errorf("project '%s' already exists at %s", args.ProjectName, projectBasePath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check project directory %s: %w", projectBasePath, err)
	}

	// --- 2. Create Project Directory ---
	if err := os.MkdirAll(projectBasePath, 0755); err != nil {
		return fmt.Errorf("failed to create project directory %s: %w", projectBasePath, err)
	}
	util.Log.Debugf("Created project directory: %s", projectBasePath)

	var success = false
	defer func() {
		if !success {
			util.Log.Warnf("Cleaning up project directory %s due to creation failure.", projectBasePath)
			_ = os.RemoveAll(projectBasePath)
		}
	}()

	// --- 3. Clone Repository ---
	if err := git.CloneRepo(args.RepoURL, repoDestPath); err != nil {
		return fmt.Errorf("failed to clone repository for project '%s': %w", args.ProjectName, err)
	}

	// --- 4. Create Project Config File ---
	appPort := args.AppPort
	if appPort <= 0 {
		appPort = 3000
	}
	nodeVersion := args.NodeVersion
	if nodeVersion == "" {
		nodeVersion = "18-alpine"
	}
	testEnvFile := args.TestEnvFile
	if testEnvFile == "" {
		testEnvFile = ".env.development"
	}
	prodEnvFile := args.ProdEnvFile
	if prodEnvFile == "" {
		prodEnvFile = ".env.production"
	}

	projCfg := config.ProjectConfig{
		ProjectName: args.ProjectName,
		GithubRepo:  args.RepoURL,
		AppPort:     appPort,
		NodeVersion: nodeVersion,
		Environments: map[string]config.ProjectEnvConfig{
			"test": {
				Domain:  args.TestDomain,
				EnvFile: testEnvFile,
			},
			"prod": {
				Domain:  args.ProdDomain,
				EnvFile: prodEnvFile,
			},
		},
		TestDomainOverride: args.TestDomain,
		ProdDomainOverride: args.ProdDomain,
	}

	if err := config.SaveProjectConfig(reflowBasePath, &projCfg); err != nil {
		return fmt.Errorf("failed to save project config for '%s': %w", args.ProjectName, err)
	}
	configFilePath := filepath.Join(projectBasePath, config.ProjectConfigFileName)
	util.Log.Infof("Created project config: %s", configFilePath)

	// --- 5. Create Initial State File ---
	initialState := config.ProjectState{
		Test: config.EnvironmentState{},
		Prod: config.EnvironmentState{},
	}
	if err := config.SaveProjectState(reflowBasePath, args.ProjectName, &initialState); err != nil {
		return fmt.Errorf("failed to save initial project state for '%s': %w", args.ProjectName, err)
	}
	stateFilePath := filepath.Join(projectBasePath, config.ProjectStateFileName)
	util.Log.Infof("Created initial project state file: %s", stateFilePath)

	// --- Log effective domains ---
	globalCfg, gerr := config.LoadGlobalConfig(reflowBasePath)
	if gerr != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(gerr, &configFileNotFoundError) {
			util.Log.Warnf("Global config file not found. Domain calculation might require manual config.")
			globalCfg = &config.GlobalConfig{Debug: util.Log.GetLevel() == logrus.DebugLevel}
		} else {
			util.Log.Warnf("Could not load global config during project creation log: %v", gerr)
		}
	}

	testEffDomain, errTest := config.GetEffectiveDomain(globalCfg, &projCfg, "test")
	if errTest != nil {
		util.Log.Warnf("Could not determine effective test domain: %v", errTest)
	}
	prodEffDomain, errProd := config.GetEffectiveDomain(globalCfg, &projCfg, "prod")
	if errProd != nil {
		util.Log.Warnf("Could not determine effective prod domain: %v", errProd)
	}

	util.Log.Info("-----------------------------------------------------")
	util.Log.Infof("✅ Project '%s' created successfully!", args.ProjectName)
	util.Log.Infof("   - Repo cloned to: %s", repoDestPath)
	util.Log.Infof("   - Config file: %s", configFilePath)
	if errTest == nil {
		util.Log.Infof("   - Test Env Domain: %s", testEffDomain)
	}
	if errProd == nil {
		util.Log.Infof("   - Prod Env Domain: %s", prodEffDomain)
	}
	util.Log.Info("-----------------------------------------------------")
	util.Log.Info("Next step: Deploy the project using 'reflow deploy ", args.ProjectName, "' or via API.")

	success = true
	return nil
}
