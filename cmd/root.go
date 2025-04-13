package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflow/cmd/deploy"
	"reflow/cmd/destroy"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"reflow/internal/config"
	"reflow/internal/util"
)

var (
	debug       bool
	cfgFileBase string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "reflow",
	Short: "Reflow is a deployment manager for Next.js applications using Docker and Nginx.",
	Long: `Reflow simplifies deploying Next.js applications onto Linux VPS instances.
It utilizes Docker for containerization and Nginx for reverse proxying,
implementing a blue-green deployment strategy to minimize downtime.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		configFlag := cmd.Flag("config").Value.String()
		if configFlag == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current working directory: %w", err)
			}
			cfgFileBase = filepath.Join(cwd, "reflow")
		} else {
			absPath, err := filepath.Abs(configFlag)
			if err != nil {
				return fmt.Errorf("failed to get absolute path for --config: %w", err)
			}
			cfgFileBase = absPath
		}

		// --- Initialize Logger Early ---
		util.InitLogger(debug)
		util.Log.Debugf("Debug flag set to: %v", debug)
		util.Log.Debugf("Using reflow base path: %s", cfgFileBase)

		// --- Check global config ONLY for debug setting override ---
		globalCfg, err := config.LoadGlobalConfig(cfgFileBase)
		if err != nil {
			var configFileNotFoundError viper.ConfigFileNotFoundError
			if errors.As(err, &configFileNotFoundError) {
				util.Log.Debugf("Global config file not found at %s. Debug setting relies on flag.", filepath.Join(cfgFileBase, config.GlobalConfigFileName))
			}
		} else {
			if globalCfg.Debug && !debug {
				util.Log.Debug("Enabling debug mode based on global config file.")
				util.InitLogger(true)
			} else if !globalCfg.Debug && debug {
				util.Log.Debug("Debug mode enabled by flag, overriding config file setting if it was false.")
			}
		}
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable verbose debug output")
	rootCmd.PersistentFlags().StringVarP(&cfgFileBase, "config", "c", "", "Base directory path for reflow configuration (default ./reflow)")

	deploy.AddDeployCommand(rootCmd)
	deploy.AddApproveCommand(rootCmd)
	destroy.AddDestroyCommand(rootCmd)
}

// GetReflowBasePath allows other commands (like init) to access the calculated base path
// AFTER PersistentPreRunE has run and determined it.
func GetReflowBasePath() string {
	if cfgFileBase == "" {
		cwd, err := os.Getwd()
		if err != nil {
			util.Log.Warnf("Could not get CWD in GetReflowBasePath fallback: %v", err)
			return "reflow"
		}
		return filepath.Join(cwd, "reflow")
	}
	return cfgFileBase
}
