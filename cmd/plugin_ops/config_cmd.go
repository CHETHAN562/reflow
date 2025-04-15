package plugin_ops

import (
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/config"
	"reflow/internal/util"

	"github.com/spf13/cobra"
)

// AddConfigCommand defines the 'plugin config' subcommands.
func AddConfigCommand(parentCmd *cobra.Command) {
	configCmd := &cobra.Command{
		Use:     "config",
		Short:   "View or edit plugin configuration",
		Aliases: []string{"cfg"},
	}

	viewCmd := &cobra.Command{
		Use:   "view <plugin-name>",
		Short: "View the configuration file for an installed plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			pluginName := args[0]
			reflowBasePath := getBasePathFromFlags(cobraCmd)

			pluginState, err := config.LoadGlobalPluginState(reflowBasePath)
			if err != nil {
				return fmt.Errorf("failed to load plugin state: %w", err)
			}

			pluginConf, ok := pluginState.InstalledPlugins[pluginName]
			if !ok {
				return fmt.Errorf("plugin '%s' not found", pluginName)
			}

			content, err := os.ReadFile(pluginConf.ConfigPath)
			if err != nil {
				if os.IsNotExist(err) {
					util.Log.Infof("Plugin '%s' config file (%s) not found or empty.", pluginName, pluginConf.ConfigPath)
					fmt.Println("{}")
					return nil
				}
				return fmt.Errorf("failed to read config file %s: %w", pluginConf.ConfigPath, err)
			}

			fmt.Println("--- Plugin Configuration ---")
			fmt.Println(string(content))
			fmt.Println("--------------------------")
			return nil
		},
	}

	editCmd := &cobra.Command{
		Use:   "edit <plugin-name>",
		Short: "Edit the configuration file for an installed plugin in $EDITOR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			pluginName := args[0]
			reflowBasePath := getBasePathFromFlags(cobraCmd)

			pluginState, err := config.LoadGlobalPluginState(reflowBasePath)
			if err != nil {
				return fmt.Errorf("failed to load plugin state: %w", err)
			}

			pluginConf, ok := pluginState.InstalledPlugins[pluginName]
			if !ok {
				return fmt.Errorf("plugin '%s' not found", pluginName)
			}

			if _, err := os.Stat(pluginConf.ConfigPath); os.IsNotExist(err) {
				if err := config.SavePluginInstanceConfig(pluginConf.ConfigPath, pluginConf.ConfigValues); err != nil {
					return fmt.Errorf("failed to create initial config file %s before editing: %w", pluginConf.ConfigPath, err)
				}
				util.Log.Debugf("Created empty config file %s for editing.", pluginConf.ConfigPath)
			} else if err != nil {
				return fmt.Errorf("error checking config file %s: %w", pluginConf.ConfigPath, err)
			}

			err = util.OpenFileInEditor(pluginConf.ConfigPath)
			if err != nil {
				return fmt.Errorf("failed to edit configuration file: %w", err)
			}

			util.Log.Debugf("Syncing updated config values from %s back to global state...", pluginConf.ConfigPath)
			currentConfigValues, loadErr := config.LoadPluginInstanceConfig(pluginConf.ConfigPath)
			if loadErr != nil {
				util.Log.Errorf("Failed to reload config from %s after edit: %v. Global state (plugins.json) NOT updated.", pluginConf.ConfigPath, loadErr)
				return fmt.Errorf("failed to reload config after edit, global state not updated")
			}
			pluginConf.ConfigValues = currentConfigValues
			if saveErr := config.SaveGlobalPluginState(reflowBasePath, pluginState); saveErr != nil {
				util.Log.Errorf("Failed to save updated global plugin state to plugins.json after editing config for '%s': %v", pluginName, saveErr)
				return fmt.Errorf("failed to save updated global state")
			}
			util.Log.Debugf("Successfully synced config changes for '%s' to plugins.json", pluginName)

			util.Log.Infof("Finished editing %s.", pluginConf.ConfigPath)
			util.Log.Warn("Note: Changes might require restarting the plugin container or reloading Reflow (future features).")
			return nil
		},
	}

	configCmd.AddCommand(viewCmd)
	configCmd.AddCommand(editCmd)
	parentCmd.AddCommand(configCmd)
}

// getBasePathFromFlags retrieves the base path for Reflow from the command line flags. (maybe combine with one in root cmd)
func getBasePathFromFlags(cobraCmd *cobra.Command) string {
	configFlag, _ := cobraCmd.Root().PersistentFlags().GetString("config")
	var reflowBasePath string
	if configFlag == "" {
		cwd, err := os.Getwd()
		if err != nil {
			util.Log.Warnf("Failed to get CWD: %v. Defaulting to './reflow'", err)
			return "reflow"
		}
		reflowBasePath = filepath.Join(cwd, "reflow")
	} else {
		absPath, err := filepath.Abs(configFlag)
		if err != nil {
			util.Log.Warnf("Failed to get absolute path for config flag '%s': %v. Using as is.", configFlag, err)
			return configFlag
		}
		reflowBasePath = absPath
	}
	util.Log.Debugf("Using reflow base path: %s", reflowBasePath)
	return reflowBasePath
}
