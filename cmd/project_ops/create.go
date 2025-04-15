package project_ops

import (
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/project"

	"github.com/spf13/cobra"
	"reflow/internal/config"
	"reflow/internal/util"
)

// AddCreateCommand defines the create command and adds it to the parent command.
func AddCreateCommand(parentCmd *cobra.Command) {
	var testDomain string
	var prodDomain string
	var appPort int
	var nodeVersion string
	var testEnvFile string
	var prodEnvFile string

	var createCmd = &cobra.Command{
		Use:   "create <project-name> <github-repo-url>",
		Short: "Create and initialize a new project in Reflow",
		Long: `Clones the specified Git repository and sets up the necessary configuration
files and directories for a new Reflow project. Uses defaults for port (3000),
node version (18-alpine), and env files (.env.development/.env.production) unless overridden by flags.

Example:
  reflow project create my-blog git@github.com:user/my-blog.git
  reflow project create my-app https://github.com/user/my-app.git --test-domain test.myapp.com --app-port 8080`,
		Args: cobra.ExactArgs(2),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			projectName := args[0]
			repoURL := args[1]

			// --- Get Base Path ---
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

			// --- Prepare Args ---
			createArgs := config.CreateProjectArgs{
				ProjectName: projectName,
				RepoURL:     repoURL,
				TestDomain:  testDomain,
				ProdDomain:  prodDomain,
				AppPort:     appPort,
				NodeVersion: nodeVersion,
				TestEnvFile: testEnvFile,
				ProdEnvFile: prodEnvFile,
			}

			// --- Call Core Logic ---
			err = project.CreateProject(reflowBasePath, createArgs)
			if err != nil {
				return err
			}

			return nil
		},
	}

	createCmd.Flags().StringVar(&testDomain, "test-domain", "", "Specify custom domain for the 'test' environment (e.g., test.myapp.com)")
	createCmd.Flags().StringVar(&prodDomain, "prod-domain", "", "Specify custom domain for the 'prod' environment (e.g., myapp.com)")
	createCmd.Flags().IntVar(&appPort, "app-port", 0, "Port the application listens on (default: 3000)")
	createCmd.Flags().StringVar(&nodeVersion, "node-version", "", "Node.js version for Docker image (default: 18-alpine)")
	createCmd.Flags().StringVar(&testEnvFile, "test-env-file", "", "Relative path to the test env file (default: .env.development)")
	createCmd.Flags().StringVar(&prodEnvFile, "prod-env-file", "", "Relative path to the prod env file (default: .env.production)")

	parentCmd.AddCommand(createCmd)
}
