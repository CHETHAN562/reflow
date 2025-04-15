package plugin_ops

import (
	"reflow/internal/plugin"
	"reflow/internal/util"

	"github.com/spf13/cobra"
)

// AddEnableCommand defines the enable command for plugins.
func AddEnableCommand(parentCmd *cobra.Command) {
	var enableCmd = &cobra.Command{
		Use:   "enable <plugin-name>",
		Short: "Enable an installed plugin",
		Long: `Marks the specified plugin as enabled. If it's a container-based plugin,
this will attempt to start its Docker container and configure Nginx.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			pluginName := args[0]
			reflowBasePath := getBasePathFromFlags(cobraCmd)

			util.Log.Debugf("Attempting to enable plugin '%s' in base path '%s'", pluginName, reflowBasePath)

			err := plugin.EnablePlugin(reflowBasePath, pluginName)
			if err != nil {
				// Specific error logged in EnablePlugin, return directly for Cobra
				return err
			}
			return nil
		},
	}
	parentCmd.AddCommand(enableCmd)
}
