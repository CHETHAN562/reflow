package orchestrator

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/plugin" // Import plugin manager
	"reflow/internal/util"
	"strings"

	dockerAPIClient "github.com/docker/docker/client"
)

// DestroyReflow stops and removes all Reflow managed containers, network, and deletes the base directory.
func DestroyReflow(ctx context.Context, reflowBasePath string, force bool) error {
	util.Log.Warn("--- Starting Reflow Destruction ---")
	util.Log.Warnf("This will stop and remove ALL Reflow managed containers (projects + nginx),")
	util.Log.Warnf("remove the '%s' Docker network,", config.ReflowNetworkName)
	util.Log.Warnf("and IRREVERSIBLY DELETE the entire Reflow base directory:")
	util.Log.Warnf("  %s", reflowBasePath)
	util.Log.Warn("This includes all configurations, states, and cloned repositories.")

	if !force {
		fmt.Printf("Are you absolutely sure you want to proceed? (Type 'yes' to confirm): ")
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read confirmation: %w", err)
		}
		if strings.TrimSpace(strings.ToLower(input)) != "yes" {
			util.Log.Info("Destruction cancelled by user.")
			return nil
		}
		util.Log.Info("Confirmation received.")
	} else {
		util.Log.Warn("Skipping confirmation due to --force flag.")
	}

	var finalErr error
	cli, err := docker.GetClient()
	if err != nil {
		return fmt.Errorf("cannot proceed with destruction: failed to get Docker client: %w", err)
	}

	// --- Stop and Remove Plugin Containers & Nginx Configs ---
	util.Log.Info("Finding and removing plugin resources...")
	pluginState, stateErr := config.LoadGlobalPluginState(reflowBasePath)
	if stateErr != nil {
		util.Log.Errorf("Failed to load plugin state during destroy: %v. Skipping plugin cleanup.", stateErr)
		finalErr = fmt.Errorf("failed to load plugin state: %w", stateErr)
	} else {
		util.Log.Infof("Checking %d installed plugin(s) for cleanup.", len(pluginState.InstalledPlugins))
		for name, pConf := range pluginState.InstalledPlugins {
			util.Log.Debugf("Cleaning up plugin: %s", name)
			if pConf.Type == config.PluginTypeContainer {
				// Stop Container
				if pConf.ContainerID != "" {
					util.Log.Warnf("Stopping plugin container %s (%s)...", name, pConf.ContainerID[:min(12, len(pConf.ContainerID))])
					_ = docker.StopContainer(ctx, pConf.ContainerID, nil) // Ignore error, try removing anyway
					// Remove Container
					util.Log.Warnf("Removing plugin container %s (%s)...", name, pConf.ContainerID[:min(12, len(pConf.ContainerID))])
					if rmErr := docker.RemoveContainer(ctx, pConf.ContainerID); rmErr != nil && !dockerAPIClient.IsErrNotFound(rmErr) {
						errMsg := fmt.Sprintf("failed to remove plugin container %s: %v", pConf.ContainerID[:min(12, len(pConf.ContainerID))], rmErr)
						util.Log.Error(errMsg)
						if finalErr == nil {
							finalErr = errors.New(errMsg)
						}
					}
				}
				// Remove Nginx Config
				if pConf.NginxConfigOk { // Check if Nginx was likely configured
					util.Log.Warnf("Removing Nginx config for plugin %s...", name)
					// Use the remove function which also handles reload implicitly (though reload is less critical during full destroy)
					if ngxRmErr := plugin.RemovePluginNginx(ctx, reflowBasePath, pConf); ngxRmErr != nil {
						util.Log.Errorf("Failed to remove Nginx config for plugin %s: %v", name, ngxRmErr)
						// Don't block destruction for Nginx config file removal failure
					}
				}
			}
		}
	}

	// --- Stop and Remove App Containers ---
	util.Log.Info("Finding and removing all Reflow managed application containers...")
	appLabels := map[string]string{docker.LabelManaged: "true", "reflow.type": "project"} // Assuming projects have a type label now or just use LabelManaged
	allAppContainers, err := docker.FindContainersByLabels(ctx, appLabels)                // Adjust label query if needed
	if err != nil {
		util.Log.Errorf("Failed to list Reflow application containers, attempting to continue: %v", err)
		if finalErr == nil {
			finalErr = fmt.Errorf("failed to list reflow app containers: %w", err)
		}
	} else {
		util.Log.Infof("Found %d managed application container(s) to remove.", len(allAppContainers))
		for _, c := range allAppContainers {
			containerName := strings.Join(c.Names, ", ")
			containerID := c.ID[:12]
			util.Log.Warnf("Stopping and removing app container %s (ID: %s)...", containerName, containerID)
			_ = docker.StopContainer(ctx, c.ID, nil)
			if rmErr := docker.RemoveContainer(ctx, c.ID); rmErr != nil {
				errMsg := fmt.Sprintf("failed to remove app container %s: %v", containerID, rmErr)
				util.Log.Error(errMsg)
				if finalErr == nil {
					finalErr = errors.New(errMsg)
				}
			}
		}
	}

	// --- Stop and Remove Nginx Container ---
	util.Log.Infof("Stopping and removing Nginx container '%s'...", config.ReflowNginxContainerName)
	_ = docker.StopContainer(ctx, config.ReflowNginxContainerName, nil)
	if rmErr := docker.RemoveContainer(ctx, config.ReflowNginxContainerName); rmErr != nil && !dockerAPIClient.IsErrNotFound(rmErr) {
		errMsg := fmt.Sprintf("failed to remove nginx container %s: %v", config.ReflowNginxContainerName, rmErr)
		util.Log.Error(errMsg)
		if finalErr == nil {
			finalErr = fmt.Errorf(errMsg)
		}
	}

	// --- Remove Network ---
	util.Log.Infof("Removing Docker network '%s'...", config.ReflowNetworkName)
	err = cli.NetworkRemove(ctx, config.ReflowNetworkName)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		errMsg := fmt.Sprintf("failed to remove network %s: %v", config.ReflowNetworkName, err)
		util.Log.Error(errMsg)
		if finalErr == nil {
			finalErr = fmt.Errorf(errMsg)
		}
	}

	// --- Delete Base Directory ---
	util.Log.Warnf("DELETING Reflow base directory: %s", reflowBasePath)
	err = os.RemoveAll(reflowBasePath)
	if err != nil {
		errMsg := fmt.Sprintf("failed to delete base directory %s: %v", reflowBasePath, err)
		util.Log.Error(errMsg)
		if finalErr == nil {
			finalErr = fmt.Errorf(errMsg)
		} else {
			finalErr = fmt.Errorf("%w; %s", finalErr, errMsg)
		}
	}

	if finalErr != nil {
		util.Log.Error("--- Reflow Destruction finished with errors ---")
		return finalErr
	}

	util.Log.Info("✅ Reflow environment destroyed successfully.")
	util.Log.Warn("--- Reflow Destruction Complete ---")
	return nil
}
