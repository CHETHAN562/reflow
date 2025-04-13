package project_ops

import (
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"reflow/internal/config"
	"reflow/internal/git"
	"reflow/internal/util"
)

// AddCreateCommand defines the create command and adds it to the parent command.
func AddCreateCommand(parentCmd *cobra.Command) {
	var testDomain string
	var prodDomain string

	var createCmd = &cobra.Command{
		Use:   "create <project-name> <github-repo-url>",
		Short: "Create and initialize a new project in Reflow",
		Long: `Clones the specified Git repository and sets up the necessary configuration
files and directories for a new Reflow project.

Example:
  reflow project create my-blog git@github.com:user/my-blog.git
  reflow project create my-app https://github.com/user/my-app.git --test-domain test.myapp.com`,
		Args: cobra.ExactArgs(2),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			projectName := args[0]
			repoURL := args[1]

			// --- Get Base Path and Load Global Config ---
			configFlag, _ := cobraCmd.Root().PersistentFlags().GetString("config")
			var reflowBasePath string
			var err error
			if configFlag == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get current working directory: %w", err)
				}
				reflowBasePath = filepath.Join(cwd, "reflow")
			} else {
				reflowBasePath, err = filepath.Abs(configFlag)
				if err != nil {
					return fmt.Errorf("failed to get absolute path for --config flag: %w", err)
				}
			}
			util.Log.Debugf("Using reflow base path: %s", reflowBasePath)

			globalCfg, err := config.LoadGlobalConfig(reflowBasePath)
			if err != nil {
				var configFileNotFoundError viper.ConfigFileNotFoundError
				if errors.As(err, &configFileNotFoundError) {
					util.Log.Warnf("Global config file not found at %s. Domain calculation might require manual config.", filepath.Join(reflowBasePath, config.GlobalConfigFileName))
					globalCfg = &config.GlobalConfig{Debug: util.Log.GetLevel() == logrus.DebugLevel}
				}
			}

			util.Log.Infof("Creating new project '%s' from repo '%s'", projectName, repoURL)

			projectBasePath := config.GetProjectBasePath(reflowBasePath, projectName)
			repoDestPath := filepath.Join(projectBasePath, config.RepoDirName)

			// --- 1. Check if Project Already Exists ---
			if _, err := os.Stat(projectBasePath); err == nil {
				return fmt.Errorf("project '%s' already exists at %s", projectName, projectBasePath)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("failed to check project directory %s: %w", projectBasePath, err)
			}

			// --- 2. Create Project Directory ---
			if err := os.MkdirAll(projectBasePath, 0755); err != nil {
				return fmt.Errorf("failed to create project directory %s: %w", projectBasePath, err)
			}
			util.Log.Debugf("Created project directory: %s", projectBasePath)

			// --- 3. Clone Repository ---
			if err := git.CloneRepo(repoURL, repoDestPath); err != nil {
				util.Log.Warnf("Cleaning up project directory %s due to clone failure.", projectBasePath)
				_ = os.RemoveAll(projectBasePath)
				return fmt.Errorf("failed to initialize project '%s': %w", projectName, err)
			}

			// --- 4. Create Project Config File ---
			projCfg := config.ProjectConfig{
				ProjectName: projectName,
				GithubRepo:  repoURL,
				AppPort:     3000,
				NodeVersion: "18-alpine",
				Environments: map[string]config.ProjectEnvConfig{
					"test": {
						Domain:  testDomain,
						EnvFile: ".env.development",
					},
					"prod": {
						Domain:  prodDomain,
						EnvFile: ".env.production",
					},
				},
			}

			if err := config.SaveProjectConfig(reflowBasePath, &projCfg); err != nil {
				return fmt.Errorf("failed to save project config for '%s': %w", projectName, err)
			}
			util.Log.Infof("Created project config: %s", filepath.Join(projectBasePath, config.ProjectConfigFileName))

			testEffDomain, errTest := config.GetEffectiveDomain(globalCfg, &projCfg, "test")
			if errTest != nil {
				util.Log.Warnf("Could not determine effective test domain: %v", errTest)
			}
			prodEffDomain, errProd := config.GetEffectiveDomain(globalCfg, &projCfg, "prod")
			if errProd != nil {
				util.Log.Warnf("Could not determine effective prod domain: %v", errProd)
			}

			// --- 5. Create Initial State File ---
			initialState := config.ProjectState{
				Test: config.EnvironmentState{},
				Prod: config.EnvironmentState{},
			}
			if err := config.SaveProjectState(reflowBasePath, projectName, &initialState); err != nil {
				return fmt.Errorf("failed to save initial project state for '%s': %w", projectName, err)
			}
			util.Log.Infof("Created initial project state file: %s", filepath.Join(projectBasePath, config.ProjectStateFileName))

			util.Log.Info("-----------------------------------------------------")
			util.Log.Infof("✅ Project '%s' created successfully!", projectName)
			util.Log.Infof("   - Repo cloned to: %s", repoDestPath)
			util.Log.Infof("   - Config file: %s", filepath.Join(projectBasePath, config.ProjectConfigFileName))
			if errTest == nil {
				util.Log.Infof("   - Test Env Domain: %s (%s)", testEffDomain, tern(testDomain != "", "from flag", tern(projCfg.Environments["test"].Domain != "", "from config", "calculated")))
			}
			if errProd == nil {
				util.Log.Infof("   - Prod Env Domain: %s (%s)", prodEffDomain, tern(prodDomain != "", "from flag", tern(projCfg.Environments["prod"].Domain != "", "from config", "calculated")))
			}
			util.Log.Info("-----------------------------------------------------")
			util.Log.Info("Next step: Deploy the project using 'reflow deploy ", projectName, "'")

			return nil
		},
	}

	createCmd.Flags().StringVar(&testDomain, "test-domain", "", "Specify custom domain for the 'test' environment (e.g., test.myapp.com)")
	createCmd.Flags().StringVar(&prodDomain, "prod-domain", "", "Specify custom domain for the 'prod' environment (e.g., myapp.com)")

	parentCmd.AddCommand(createCmd)
}

func tern(condition bool, trueVal, falseVal string) string {
	if condition {
		return trueVal
	}
	return falseVal
}
