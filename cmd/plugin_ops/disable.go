package plugin_ops

import (
	"reflow/internal/plugin"
	"reflow/internal/util"

	"github.com/spf13/cobra"
)

// AddDisableCommand defines the disable command for plugins.
func AddDisableCommand(parentCmd *cobra.Command) {
	var disableCmd = &cobra.Command{
		Use:   "disable <plugin-name>",
		Short: "Disable an installed plugin",
		Long: `Marks the specified plugin as disabled. If it's a container-based plugin,
this will attempt to stop its Docker container and remove its Nginx configuration.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			pluginName := args[0]
			reflowBasePath := getBasePathFromFlags(cobraCmd)

			util.Log.Debugf("Attempting to disable plugin '%s' in base path '%s'", pluginName, reflowBasePath)

			err := plugin.DisablePlugin(reflowBasePath, pluginName)
			if err != nil {
				return err
			}
			return nil
		},
	}
	parentCmd.AddCommand(disableCmd)
}
