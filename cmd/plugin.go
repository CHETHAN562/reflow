package cmd

import (
	"github.com/spf13/cobra"
	"reflow/cmd/plugin_ops"
)

// pluginCmd represents the base command for plugin operations
var pluginCmd = &cobra.Command{
	Use:     "plugin",
	Short:   "Manage Reflow plugins (install, list, etc.)",
	Long:    `Provides subcommands to install, manage, and configure Reflow plugins.`,
	Aliases: []string{"plugins"},
}

func init() {
	rootCmd.AddCommand(pluginCmd)

	plugin_ops.AddInstallCommand(pluginCmd)
	plugin_ops.AddListCommand(pluginCmd)
	plugin_ops.AddUninstallCommand(pluginCmd)
	plugin_ops.AddConfigCommand(pluginCmd)
	plugin_ops.AddEnableCommand(pluginCmd)
	plugin_ops.AddDisableCommand(pluginCmd)
}
