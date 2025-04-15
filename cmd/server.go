package cmd

import (
	"github.com/spf13/cobra"
	"reflow/internal/api"
	"reflow/internal/util"
)

// AddServerCommand adds the server command group.
func AddServerCommand(rootCmd *cobra.Command) {
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the Reflow internal API server",
		Long:  `Provides commands to manage the internal API server used for plugin communication.`,
	}

	var host string
	var port string

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the internal API server",
		Long: `Starts the local HTTP server that plugins (like the dashboard) can use
to interact with Reflow's core functions. Intended for local access only.`,
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			basePath := GetReflowBasePath()
			util.Log.Debugf("Using reflow base path for server: %s", basePath)

			err := api.StartServer(basePath, host, port)
			if err != nil {
				return err
			}
			return nil
		},
	}

	startCmd.Flags().StringVar(&host, "host", "localhost", "Host address for the API server to bind to")
	startCmd.Flags().StringVar(&port, "port", "8585", "Port for the API server to listen on")

	serverCmd.AddCommand(startCmd)
	rootCmd.AddCommand(serverCmd)
}
