package project_ops

import (
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/config"
	"reflow/internal/util"

	"github.com/spf13/cobra"
)

// AddConfigCommand defines the 'config' parent command and its subcommands.
func AddConfigCommand(parentCmd *cobra.Command) {
	// --- Parent 'config' Command ---
	var configCmd = &cobra.Command{
		Use:     "config",
		Short:   "View or edit project configuration",
		Long:    `Provides subcommands to view or edit the configuration file (config.yaml) for a specific project.`,
		Aliases: []string{"cfg"},
	}

	// --- 'config view' Subcommand ---
	var viewCmd = &cobra.Command{
		Use:   "view <project-name>",
		Short: "View the configuration file for a project",
		Long:  `Displays the raw content of the reflow/apps/<project-name>/config.yaml file.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			projectName := args[0]

			configFlag, _ := cobraCmd.Root().PersistentFlags().GetString("config")
			var reflowBasePath string
			var pathErr error
			if configFlag == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get current working directory: %w", err)
				}
				reflowBasePath = filepath.Join(cwd, "reflow")
			} else {
				reflowBasePath, pathErr = filepath.Abs(configFlag)
				if pathErr != nil {
					return fmt.Errorf("failed to get absolute path for --config flag: %w", pathErr)
				}
			}

			projectBasePath := config.GetProjectBasePath(reflowBasePath, projectName)
			configFilePath := filepath.Join(projectBasePath, config.ProjectConfigFileName)

			util.Log.Debugf("Attempting to read config file: %s", configFilePath)

			content, err := os.ReadFile(configFilePath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("project '%s' configuration file not found at %s (run 'reflow project create' first?)", projectName, configFilePath)
				}
				return fmt.Errorf("failed to read config file %s: %w", configFilePath, err)
			}

			fmt.Println("--- Configuration File Content ---")
			fmt.Println(string(content))
			fmt.Println("---------------------------------")

			return nil
		},
	}

	// --- 'config edit' Subcommand ---
	var editCmd = &cobra.Command{
		Use:   "edit <project-name>",
		Short: "Edit the configuration file for a project in $EDITOR",
		Long: `Opens the project's configuration file (reflow/apps/<project-name>/config.yaml)
in the editor specified by the $EDITOR environment variable (or a common default like vim/nano).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			projectName := args[0]

			configFlag, _ := cobraCmd.Root().PersistentFlags().GetString("config")
			var reflowBasePath string
			var pathErr error
			if configFlag == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get current working directory: %w", err)
				}
				reflowBasePath = filepath.Join(cwd, "reflow")
			} else {
				reflowBasePath, pathErr = filepath.Abs(configFlag)
				if pathErr != nil {
					return fmt.Errorf("failed to get absolute path for --config flag: %w", pathErr)
				}
			}

			projectBasePath := config.GetProjectBasePath(reflowBasePath, projectName)
			configFilePath := filepath.Join(projectBasePath, config.ProjectConfigFileName)

			if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
				return fmt.Errorf("project '%s' configuration file not found at %s (run 'reflow project create' first?)", projectName, configFilePath)
			} else if err != nil {
				return fmt.Errorf("error checking config file %s: %w", configFilePath, err)
			}

			err := util.OpenFileInEditor(configFilePath)
			if err != nil {
				return fmt.Errorf("failed to edit configuration file")
			}

			util.Log.Infof("Successfully finished editing %s.", configFilePath)
			util.Log.Warn("Note: Changes to config (e.g., nodeVersion, appPort) may require a new 'reflow deploy' to take effect.")
			return nil
		},
	}

	configCmd.AddCommand(viewCmd)
	configCmd.AddCommand(editCmd)

	parentCmd.AddCommand(configCmd)
}
