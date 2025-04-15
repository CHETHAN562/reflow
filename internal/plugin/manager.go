package plugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"os/exec"
	"path/filepath"
	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/git"
	"reflow/internal/nginx"
	"reflow/internal/util"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// InstallPlugin installs a plugin from a Git repository.
func InstallPlugin(reflowBasePath, repoURL string) error {
	util.Log.Infof("Attempting to install plugin from repository: %s", repoURL)
	ctx := context.Background() // Use background context for install operations

	// --- 1. Determine Plugin Name and Install Path ---
	pluginName, err := DerivePluginName(repoURL)
	if err != nil {
		return fmt.Errorf("could not determine plugin name: %w", err)
	}
	installPath := config.GetPluginInstallPath(reflowBasePath, pluginName)
	util.Log.Debugf("Derived plugin name: %s", pluginName)
	util.Log.Debugf("Target installation path: %s", installPath)

	// --- 2. Check if Already Installed ---
	globalState, err := config.LoadGlobalPluginState(reflowBasePath)
	if err != nil {
		return fmt.Errorf("failed to load global plugin state: %w", err)
	}
	if _, exists := globalState.InstalledPlugins[pluginName]; exists {
		return fmt.Errorf("plugin '%s' is already installed. Uninstall first if you want to reinstall", pluginName)
	}

	// --- 3. Clone Repository ---
	pluginsBasePath := config.GetPluginsBasePath(reflowBasePath)
	if err := os.MkdirAll(pluginsBasePath, 0755); err != nil {
		return fmt.Errorf("failed to create plugins directory %s: %w", pluginsBasePath, err)
	}

	if err := git.CloneRepo(repoURL, installPath); err != nil {
		_ = os.RemoveAll(installPath)
		return fmt.Errorf("failed to clone plugin repository '%s': %w", repoURL, err)
	}

	// --- 4. Parse Plugin Metadata ---
	metadataPath := filepath.Join(installPath, config.PluginMetadataFileName)
	metadata, err := ParsePluginMetadata(metadataPath)
	if err != nil {
		_ = os.RemoveAll(installPath)
		return fmt.Errorf("failed to parse plugin metadata file (%s): %w", config.PluginMetadataFileName, err)
	}
	util.Log.Infof("Loaded metadata for plugin '%s' (Type: %s, Version: %s)", metadata.Name, metadata.Type, metadata.Version)

	// --- 5. Run Setup Prompts and Collect Config ---
	configValues := make(map[string]string)
	if len(metadata.Setup) > 0 {
		util.Log.Info("Running plugin setup configuration...")
		reader := bufio.NewReader(os.Stdin)
		for _, prompt := range metadata.Setup {
			// Generate default value dynamically if needed (e.g., domain)
			defaultValue := prompt.Default
			if prompt.Key == "domain" && defaultValue == "" {
				// Attempt to calculate a default plugin domain
				globalCfg, _ := config.LoadGlobalConfig(reflowBasePath) // Ignore error, GetEffectivePluginDomain handles nil
				calculatedDomain, domainErr := GetEffectivePluginDomain(globalCfg, pluginName, "plugin")
				if domainErr == nil {
					defaultValue = calculatedDomain
				}
			}

			fmt.Printf("  - %s", prompt.Prompt)
			if defaultValue != "" {
				fmt.Printf(" [%s]", defaultValue)
			}
			if prompt.Description != "" {
				fmt.Printf("\n    (%s)", prompt.Description)
			}
			fmt.Print(": ")

			input, _ := reader.ReadString('\n')
			value := strings.TrimSpace(input)

			if value == "" && defaultValue != "" {
				value = defaultValue
			}

			if value == "" && prompt.Required {
				fmt.Println("  This field is required.")
				fmt.Printf("  - %s: ", prompt.Prompt)
				input, _ = reader.ReadString('\n')
				value = strings.TrimSpace(input)
				if value == "" {
					_ = os.RemoveAll(installPath)
					return fmt.Errorf("required configuration value '%s' was not provided", prompt.Key)
				}
			}
			configValues[prompt.Key] = value
		}
	}

	// --- 6. Save Instance Configuration ---
	instanceConfigPath := config.GetPluginConfigPath(reflowBasePath, pluginName)
	if err := config.SavePluginInstanceConfig(instanceConfigPath, configValues); err != nil {
		_ = os.RemoveAll(installPath)
		return fmt.Errorf("failed to save plugin instance configuration: %w", err)
	}

	// --- 7. Prepare Global State Entry ---
	instanceConfig := &config.PluginInstanceConfig{
		PluginName:   pluginName,
		DisplayName:  metadata.Name,
		RepoURL:      repoURL,
		Version:      metadata.Version,
		InstallPath:  installPath,
		ConfigPath:   instanceConfigPath,
		Type:         metadata.Type,
		ConfigValues: configValues,
		Enabled:      true,
		InstallTime:  time.Now(),
		Metadata:     metadata, // Keep metadata temporarily for post-install
	}

	// --- 8. Post-Install Actions (Container Start, Nginx Update) ---
	if metadata.Type == config.PluginTypeContainer {
		util.Log.Info("Performing setup for container plugin...")

		// Start Container
		containerID, startErr := startPluginContainer(ctx, reflowBasePath, instanceConfig, configValues)
		if startErr != nil {
			_ = os.RemoveAll(installPath) // Cleanup install path on container start failure
			return fmt.Errorf("failed to start plugin container: %w", startErr)
		}
		instanceConfig.ContainerID = containerID
		util.Log.Infof("Plugin container started successfully (ID: %s)", containerID[:12])

		// Configure Nginx (if applicable)
		if metadata.Nginx != nil {
			nginxErr := configurePluginNginx(ctx, reflowBasePath, instanceConfig)
			if nginxErr != nil {
				// Attempt rollback: Stop and remove container, remove install dir
				util.Log.Errorf("Failed to configure Nginx for plugin: %v", nginxErr)
				util.Log.Warnf("Attempting rollback: stopping container %s...", containerID[:12])
				_ = docker.StopContainer(ctx, containerID, nil)
				util.Log.Warnf("Attempting rollback: removing container %s...", containerID[:12])
				_ = docker.RemoveContainer(ctx, containerID)
				util.Log.Warnf("Attempting rollback: removing installation directory %s...", installPath)
				_ = os.RemoveAll(installPath)
				return fmt.Errorf("failed to configure Nginx for plugin: %w", nginxErr)
			}
			instanceConfig.NginxConfigOk = true
			util.Log.Info("Nginx configured successfully for plugin.")
		} else {
			util.Log.Info("Plugin metadata does not specify Nginx configuration. Skipping Nginx setup.")
		}
	}

	// --- 9. Save Final Global Plugin State ---
	instanceConfig.Metadata = nil // Don't save full metadata in state file
	globalState.InstalledPlugins[pluginName] = instanceConfig
	if err := config.SaveGlobalPluginState(reflowBasePath, globalState); err != nil {
		// This is still problematic, but less critical now as container/nginx might be running
		util.Log.Errorf("CRITICAL: Plugin installed and setup completed, but failed to save final global plugin state: %v", err)
		util.Log.Warn("Reflow might not recognize the plugin correctly. Manual state update might be needed in plugins.json.")
	}

	util.Log.Infof("✅ Successfully installed and configured plugin '%s' (from %s)!", metadata.Name, pluginName)
	if instanceConfig.Type == config.PluginTypeContainer && instanceConfig.NginxConfigOk {
		domain, domainErr := GetEffectivePluginDomainFromConfig(reflowBasePath, instanceConfig)
		if domainErr == nil {
			util.Log.Infof("   Access URL: %s (Ensure DNS points to server IP!)", domain)
		} else {
			util.Log.Warnf("   Could not determine access URL: %v", domainErr)
		}
	}
	return nil
}

// UninstallPlugin removes an installed plugin.
func UninstallPlugin(reflowBasePath, pluginName string) error {
	util.Log.Warnf("Attempting to uninstall plugin '%s'...", pluginName)
	ctx := context.Background()

	// --- 1. Load State ---
	globalState, err := config.LoadGlobalPluginState(reflowBasePath)
	if err != nil {
		return fmt.Errorf("failed to load global plugin state: %w", err)
	}

	pluginConfig, exists := globalState.InstalledPlugins[pluginName]
	if !exists {
		return fmt.Errorf("plugin '%s' is not installed", pluginName)
	}

	// --- 2. Stop Container (if applicable) ---
	if pluginConfig.Type == config.PluginTypeContainer && pluginConfig.ContainerID != "" {
		util.Log.Infof("Stopping container %s for plugin '%s'...", pluginConfig.ContainerID[:12], pluginName)
		if err := stopPluginContainer(ctx, reflowBasePath, pluginConfig); err != nil {
			// Log error but continue uninstall attempt
			util.Log.Errorf("Failed to stop container %s during uninstall: %v. Continuing cleanup.", pluginConfig.ContainerID[:12], err)
		}
	}

	// --- 3. Remove Nginx Config (if applicable) ---
	if pluginConfig.Type == config.PluginTypeContainer && pluginConfig.NginxConfigOk {
		util.Log.Infof("Removing Nginx configuration for plugin '%s'...", pluginName)
		if err := RemovePluginNginx(ctx, reflowBasePath, pluginConfig); err != nil {
			// Log error but continue uninstall attempt
			util.Log.Errorf("Failed to remove Nginx config during uninstall: %v. Continuing cleanup.", err)
		}
	}

	// --- 4. Remove Container (if applicable) ---
	if pluginConfig.Type == config.PluginTypeContainer && pluginConfig.ContainerID != "" {
		util.Log.Infof("Removing container %s for plugin '%s'...", pluginConfig.ContainerID[:12], pluginName)
		if err := docker.RemoveContainer(ctx, pluginConfig.ContainerID); err != nil {
			// Log error but continue uninstall attempt
			util.Log.Errorf("Failed to remove container %s during uninstall: %v. Continuing cleanup.", pluginConfig.ContainerID[:12], err)
		}
	}

	// --- 5. Remove Installation Directory ---
	util.Log.Infof("Removing installation directory: %s", pluginConfig.InstallPath)
	if err := os.RemoveAll(pluginConfig.InstallPath); err != nil {
		// Log error but continue uninstall attempt
		util.Log.Errorf("Failed to remove installation directory %s: %v. Continuing cleanup.", pluginConfig.InstallPath, err)
	}

	// --- 6. Update Global State ---
	delete(globalState.InstalledPlugins, pluginName)
	if err := config.SaveGlobalPluginState(reflowBasePath, globalState); err != nil {
		// This is bad, state is inconsistent with filesystem
		return fmt.Errorf("failed to save updated global plugin state after uninstalling '%s': %w. Manual cleanup of plugins.json may be required", pluginName, err)
	}

	util.Log.Infof("✅ Successfully uninstalled plugin '%s'.", pluginName)
	return nil
}

// startPluginContainer builds (if needed) and starts a container for a plugin.
func startPluginContainer(ctx context.Context, reflowBasePath string, pluginConf *config.PluginInstanceConfig, currentConfigValues map[string]string) (string, error) {
	if pluginConf.Metadata == nil || pluginConf.Metadata.Container == nil {
		return "", errors.New("plugin metadata or container config is missing")
	}
	containerMeta := pluginConf.Metadata.Container

	imageName := containerMeta.Image
	var finalImageName string

	if containerMeta.Dockerfile != "" {
		dockerfilePath := filepath.Join(pluginConf.InstallPath, containerMeta.Dockerfile)
		contextPath := pluginConf.InstallPath
		imageTag := fmt.Sprintf("reflow-plugin-%s:%s", pluginConf.PluginName, pluginConf.Version)

		util.Log.Infof("Building Docker image for plugin '%s' using Dockerfile: %s", pluginConf.DisplayName, containerMeta.Dockerfile)
		buildArgs := make(map[string]*string)
		for key, val := range containerMeta.BuildArgs {
			v := val
			buildArgs[key] = &v
		}

		err := docker.BuildImage(ctx, dockerfilePath, contextPath, imageTag, buildArgs)
		if err != nil {
			return "", fmt.Errorf("docker image build failed for plugin '%s': %w", pluginConf.PluginName, err)
		}
		finalImageName = imageTag
	} else if imageName != "" {
		util.Log.Infof("Pulling image '%s' for plugin '%s'...", imageName, pluginConf.DisplayName)
		err := docker.PullImage(ctx, imageName)
		if err != nil {
			return "", fmt.Errorf("failed to pull image '%s': %w", imageName, err)
		}
		finalImageName = imageName
	} else {
		return "", errors.New("container metadata must specify 'dockerfile' or 'image'")
	}

	containerName := fmt.Sprintf("reflow-plugin-%s", pluginConf.PluginName)

	envVars := []string{}
	for key, valTmpl := range containerMeta.Env {
		val := valTmpl
		if strings.Contains(val, "{{") {
			for cfgKey, cfgVal := range currentConfigValues { // Use currentConfigValues
				placeholder := fmt.Sprintf("{{config.%s}}", cfgKey)
				val = strings.ReplaceAll(val, placeholder, cfgVal)
			}
		}
		envVars = append(envVars, fmt.Sprintf("%s=%s", key, val))
	}

	appPort := 0
	if portStr, ok := pluginConf.ConfigValues["containerPort"]; ok {
		fmt.Sscan(portStr, &appPort)
	}
	if appPort == 0 && pluginConf.Metadata.Nginx != nil {
		appPort = pluginConf.Metadata.Nginx.ContainerPort
	}
	if appPort == 0 {
		util.Log.Warnf("Could not determine application port for plugin %s, defaulting to 8080. Nginx/Health checks may fail if incorrect.", pluginConf.PluginName)
		appPort = 8080
	}
	envVars = append(envVars, fmt.Sprintf("PORT=%d", appPort))

	labels := map[string]string{
		docker.LabelManaged:  "true",
		"reflow.type":        "plugin",
		"reflow.plugin.name": pluginConf.PluginName,
	}

	runOptions := docker.ContainerRunOptions{
		ImageName:     finalImageName,
		ContainerName: containerName,
		NetworkName:   config.ReflowNetworkName,
		Labels:        labels,
		EnvVars:       envVars,
		AppPort:       appPort,
		RestartPolicy: "unless-stopped",
	}

	cli, _ := docker.GetClient()
	_, inspectErr := cli.ContainerInspect(ctx, containerName)
	if inspectErr == nil {
		util.Log.Warnf("Container '%s' already exists. Stopping and removing before creating new one.", containerName)
		_ = docker.StopContainer(ctx, containerName, nil)
		if rmErr := docker.RemoveContainer(ctx, containerName); rmErr != nil {
			return "", fmt.Errorf("failed to remove existing container '%s': %w", containerName, rmErr)
		}
	} else if !docker.IsErrNotFound(inspectErr) { // Use helper IsErrNotFound
		return "", fmt.Errorf("failed to inspect existing container '%s': %w", containerName, inspectErr)
	}

	containerID, err := docker.RunContainer(ctx, runOptions)
	if err != nil {
		return "", fmt.Errorf("failed to run container %s: %w", containerName, err)
	}

	time.Sleep(5 * time.Second)

	return containerID, nil
}

// stopPluginContainer stops the container associated with a plugin.
func stopPluginContainer(ctx context.Context, reflowBasePath string, pluginConf *config.PluginInstanceConfig) error {
	if pluginConf.ContainerID == "" {
		util.Log.Warnf("Plugin '%s' has no associated container ID to stop.", pluginConf.PluginName)
		return nil
	}
	err := docker.StopContainer(ctx, pluginConf.ContainerID, nil)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "container not running") || docker.IsErrNotFound(err) {
			util.Log.Warnf("Container %s for plugin '%s' was already stopped or not found.", pluginConf.ContainerID[:min(12, len(pluginConf.ContainerID))], pluginConf.PluginName)
			return nil
		}
		return fmt.Errorf("failed to stop container %s for plugin %s: %w", pluginConf.ContainerID[:min(12, len(pluginConf.ContainerID))], pluginConf.PluginName, err)
	}
	util.Log.Infof("Stopped container %s for plugin %s", pluginConf.ContainerID[:min(12, len(pluginConf.ContainerID))], pluginConf.PluginName)
	return nil
}

// configurePluginNginx generates/writes Nginx config for a plugin and reloads Nginx.
func configurePluginNginx(ctx context.Context, reflowBasePath string, pluginConf *config.PluginInstanceConfig) error {
	if pluginConf.Metadata == nil || pluginConf.Metadata.Nginx == nil {
		return errors.New("plugin metadata or nginx config is missing")
	}
	nginxMeta := pluginConf.Metadata.Nginx

	domain, err := GetEffectivePluginDomainFromConfig(reflowBasePath, pluginConf)
	if err != nil {
		return fmt.Errorf("failed to determine domain for plugin '%s': %w", pluginConf.PluginName, err)
	}

	containerPort := nginxMeta.ContainerPort
	if containerPort == 0 {
		if portStr, ok := pluginConf.ConfigValues["containerPort"]; ok {
			fmt.Sscan(portStr, &containerPort)
		}
	}
	if containerPort == 0 {
		return fmt.Errorf("could not determine container port for plugin '%s' Nginx config", pluginConf.PluginName)
	}

	containerName := fmt.Sprintf("reflow-plugin-%s", pluginConf.PluginName)

	var nginxConfContent string
	if nginxMeta.CustomTemplatePath != "" {
		templatePath := filepath.Join(pluginConf.InstallPath, nginxMeta.CustomTemplatePath)
		util.Log.Debugf("Using custom Nginx template for plugin '%s': %s", pluginConf.PluginName, templatePath)
		// TODO: Implement custom template rendering using pluginConf.ConfigValues
		return fmt.Errorf("custom Nginx template rendering not yet implemented")
	} else if nginxMeta.UseDefaultTemplate || (nginxMeta.CustomTemplatePath == "" && domain != "" && containerPort != 0) {
		util.Log.Debugf("Using default Nginx template for plugin '%s'", pluginConf.PluginName)
		nginxData := nginx.PluginTemplateData{
			PluginName:    pluginConf.PluginName,
			ContainerName: containerName,
			Domain:        domain,
			AppPort:       containerPort,
			Config:        pluginConf.ConfigValues,
		}
		content, genErr := nginx.GenerateNginxPluginConfig(nginxData)
		if genErr != nil {
			return fmt.Errorf("failed to generate default Nginx config for plugin '%s': %w", pluginConf.PluginName, genErr)
		}
		nginxConfContent = content
	} else {
		return fmt.Errorf("cannot configure Nginx for plugin '%s': insufficient metadata (need template path, or useDefaultTemplate=true with domain/port)", pluginConf.PluginName)
	}

	// Write Nginx Config File (use a distinct naming convention)
	confFileName := fmt.Sprintf("plugin.%s.conf", pluginConf.PluginName)
	if err := nginx.WriteNginxPluginConfig(reflowBasePath, confFileName, nginxConfContent); err != nil {
		return fmt.Errorf("failed to write Nginx config for plugin '%s': %w", pluginConf.PluginName, err)
	}

	// Reload Nginx
	if err := nginx.ReloadNginx(ctx); err != nil {
		nginxConfPath := filepath.Join(reflowBasePath, config.NginxDirName, config.NginxConfDirName, confFileName)
		_ = os.Remove(nginxConfPath)
		return fmt.Errorf("failed to reload Nginx after updating config for plugin '%s': %w", pluginConf.PluginName, err)
	}

	return nil
}

// RemovePluginNginx removes the Nginx config file for a plugin and reloads Nginx.
func RemovePluginNginx(ctx context.Context, reflowBasePath string, pluginConf *config.PluginInstanceConfig) error {
	confFileName := fmt.Sprintf("plugin.%s.conf", pluginConf.PluginName)
	nginxConfPath := filepath.Join(reflowBasePath, config.NginxDirName, config.NginxConfDirName, confFileName)

	err := os.Remove(nginxConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			util.Log.Warnf("Nginx config file for plugin '%s' not found at %s, nothing to remove.", pluginConf.PluginName, nginxConfPath)
		} else {
			return fmt.Errorf("failed to remove Nginx config file %s: %w", nginxConfPath, err)
		}
	} else {
		util.Log.Infof("Removed Nginx config file: %s", nginxConfPath)
	}

	if reloadErr := nginx.ReloadNginx(ctx); reloadErr != nil {
		util.Log.Errorf("Failed to reload Nginx after removing config for plugin '%s': %v", pluginConf.PluginName, reloadErr)
	}

	return nil
}

// ParsePluginMetadata reads and parses the plugin metadata file.
func ParsePluginMetadata(filePath string) (*config.PluginMetadata, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("metadata file '%s' not found in plugin repository", config.PluginMetadataFileName)
		}
		return nil, fmt.Errorf("failed to read metadata file %s: %w", filePath, err)
	}

	var metadata config.PluginMetadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse YAML metadata file %s: %w", filePath, err)
	}

	// --- Validation ---
	if metadata.Name == "" {
		return nil, errors.New("plugin metadata is missing required field: name")
	}
	if metadata.Version == "" {
		return nil, errors.New("plugin metadata is missing required field: version")
	}
	if metadata.Type != config.PluginTypeCLI && metadata.Type != config.PluginTypeContainer {
		return nil, fmt.Errorf("plugin metadata has invalid type '%s': must be '%s' or '%s'", metadata.Type, config.PluginTypeCLI, config.PluginTypeContainer)
	}
	if metadata.Type == config.PluginTypeContainer {
		if metadata.Container == nil {
			return nil, errors.New("container plugin metadata must include a 'container' section")
		}
		if metadata.Container.Dockerfile == "" && metadata.Container.Image == "" {
			return nil, errors.New("container plugin metadata must specify either 'container.dockerfile' or 'container.image'")
		}
	}
	if metadata.Type == config.PluginTypeCLI && metadata.Commands == nil {
		return nil, errors.New("cli plugin metadata must include a 'commands' section")
	}
	if metadata.Commands != nil && metadata.Commands.Executable == "" {
		return nil, errors.New("cli plugin 'commands' section requires 'executable' path")
	}

	return &metadata, nil
}

// DerivePluginName attempts to generate a simple, filesystem-friendly name
func DerivePluginName(repoURL string) (string, error) {
	if repoURL == "" {
		return "", errors.New("repository URL cannot be empty")
	}
	name := strings.TrimPrefix(repoURL, "https://")
	name = strings.TrimPrefix(name, "http://")
	name = strings.TrimPrefix(name, "git@")
	name = strings.TrimSuffix(name, ".git")
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.TrimPrefix(name, "github_com_")
	name = strings.TrimPrefix(name, "gitlab_com_")
	name = strings.TrimPrefix(name, "bitbucket_org_")

	if name == "" {
		return "", fmt.Errorf("could not derive a valid name from URL '%s'", repoURL)
	}

	// Allow only a-z, 0-9, -, _
	sanitizedName := ""
	hasValidChar := false
	for _, r := range strings.ToLower(name) { // Convert to lowercase first
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			sanitizedName += string(r)
			hasValidChar = true
		}
	}

	if !hasValidChar {
		return "", fmt.Errorf("no valid characters found after sanitizing URL '%s'", repoURL)
	}

	sanitizedName = strings.Trim(sanitizedName, "-_")

	if sanitizedName == "" {
		return "", fmt.Errorf("derived name is empty after trimming separators from URL '%s'", repoURL)
	}

	return sanitizedName, nil
}

// ListInstalledPlugins retrieves the list of installed plugins.
func ListInstalledPlugins(reflowBasePath string) ([]*config.PluginInstanceConfig, error) {
	globalState, err := config.LoadGlobalPluginState(reflowBasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load global plugin state: %w", err)
	}

	plugins := make([]*config.PluginInstanceConfig, 0, len(globalState.InstalledPlugins))
	for _, pluginConf := range globalState.InstalledPlugins {
		plugins = append(plugins, pluginConf)
	}

	return plugins, nil
}

// GetEffectivePluginDomainFromConfig calculates the domain using saved plugin config.
func GetEffectivePluginDomainFromConfig(reflowBasePath string, pluginConf *config.PluginInstanceConfig) (string, error) {
	if domain, ok := pluginConf.ConfigValues["domain"]; ok && domain != "" {
		util.Log.Debugf("Using configured domain for plugin %s: %s", pluginConf.PluginName, domain)
		return domain, nil
	}
	// Fallback to calculated domain
	globalCfg, err := config.LoadGlobalConfig(reflowBasePath)
	if err != nil {
		util.Log.Warnf("Could not load global config while getting plugin domain: %v. Domain calculation might fail.", err)
	}
	return GetEffectivePluginDomain(globalCfg, pluginConf.PluginName, "plugin") // Use generic env "plugin"
}

// GetEffectivePluginDomain calculates the domain name for a plugin.
// env parameter is usually "plugin" but could be adapted if plugins have envs later.
func GetEffectivePluginDomain(globalCfg *config.GlobalConfig, pluginName, env string) (string, error) {
	if globalCfg == nil || globalCfg.DefaultDomain == "" || globalCfg.DefaultDomain == "localhost" || globalCfg.DefaultDomain == "yourdomain.com" {
		return "", fmt.Errorf("cannot calculate default domain for plugin %s: global defaultDomain is not set or invalid ('%s')", pluginName, globalCfg.DefaultDomain)
	}
	// Simple structure: plugin-<name>.<defaultDomain>
	calculatedDomain := fmt.Sprintf("%s-%s.%s", pluginName, env, globalCfg.DefaultDomain)
	util.Log.Debugf("Using calculated default domain for plugin %s/%s: %s", pluginName, env, calculatedDomain)
	return calculatedDomain, nil
}

// Helper for min function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// LoadCliPlugins dynamically adds commands from enabled CLI plugins to the root command.
func LoadCliPlugins(reflowBasePath string, rootCommand *cobra.Command) error {
	util.Log.Debug("Scanning for enabled CLI plugins to load commands...")
	globalState, err := config.LoadGlobalPluginState(reflowBasePath)
	if err != nil {
		// Don't fail startup if plugin state is unloadable, just log it.
		util.Log.Errorf("Failed to load global plugin state while loading CLI plugins: %v. Skipping CLI plugin loading.", err)
		return nil // Return nil to allow Reflow to continue starting
	}

	loadedCount := 0
	for pluginName, pluginConf := range globalState.InstalledPlugins {
		if pluginConf.Enabled && pluginConf.Type == config.PluginTypeCLI {
			util.Log.Debugf("Loading CLI plugin: %s", pluginName)

			metadataPath := filepath.Join(pluginConf.InstallPath, config.PluginMetadataFileName)
			metadata, parseErr := ParsePluginMetadata(metadataPath)
			if parseErr != nil {
				util.Log.Warnf("Could not parse metadata for enabled CLI plugin '%s': %v. Skipping.", pluginName, parseErr)
				continue
			}

			if metadata.Commands == nil || metadata.Commands.Executable == "" || len(metadata.Commands.Definitions) == 0 {
				util.Log.Warnf("Enabled CLI plugin '%s' has incomplete command definitions in metadata. Skipping.", pluginName)
				continue
			}

			executablePath := filepath.Join(pluginConf.InstallPath, metadata.Commands.Executable)
			if !strings.HasPrefix(executablePath, pluginConf.InstallPath) {
				util.Log.Errorf("Security risk: Executable path '%s' for plugin '%s' is outside its installation directory. Skipping.", metadata.Commands.Executable, pluginName)
				continue
			}

			for cmdName, cmdDesc := range metadata.Commands.Definitions {
				foundCmd, _, err := rootCommand.Find([]string{cmdName})
				if err == nil && foundCmd != nil {
					util.Log.Warnf("Plugin '%s' command '%s' conflicts with an existing command. Skipping.", pluginName, cmdName)
					continue
				}

				pluginCobraCmd := &cobra.Command{
					Use:   cmdName,
					Short: fmt.Sprintf("[%s Plugin] %s", pluginConf.DisplayName, cmdDesc),
					Long:  fmt.Sprintf("Command provided by the '%s' plugin (%s).\nExecutes: %s", pluginConf.DisplayName, pluginName, metadata.Commands.Executable),
					RunE: func(cmd *cobra.Command, args []string) error {
						util.Log.Debugf("Executing command '%s' from plugin '%s'...", cmdName, pluginName)
						util.Log.Debugf(" Plugin Executable: %s", executablePath)
						util.Log.Debugf(" Arguments: %v", args)

						execCmd := exec.Command(executablePath, args...)
						execCmd.Stdin = os.Stdin
						execCmd.Stdout = os.Stdout
						execCmd.Stderr = os.Stderr
						execCmd.Env = append(os.Environ(),
							fmt.Sprintf("REFLOW_BASE_PATH=%s", reflowBasePath),
							fmt.Sprintf("REFLOW_PLUGIN_CONFIG_PATH=%s", pluginConf.ConfigPath),
							fmt.Sprintf("REFLOW_PLUGIN_INSTALL_PATH=%s", pluginConf.InstallPath),
						)

						err := execCmd.Run()
						if err != nil {
							// Don't wrap the error here, let Cobra handle the exit code from the plugin
							util.Log.Debugf("Plugin command '%s' execution finished with error: %v", cmdName, err)
							return err
						}
						util.Log.Debugf("Plugin command '%s' execution successful.", cmdName)
						return nil
					},
					// Allow arbitrary arguments to be passed to the plugin executable
					DisableFlagParsing: true,
				}
				rootCommand.AddCommand(pluginCobraCmd)
				loadedCount++
				util.Log.Debugf("Added command '%s' from plugin '%s'", cmdName, pluginName)
			}
		}
	}
	if loadedCount > 0 {
		util.Log.Infof("Loaded %d command(s) from enabled CLI plugins.", loadedCount)
	} else {
		util.Log.Debug("No enabled CLI plugins with valid commands found.")
	}
	return nil
}

// EnablePlugin enables a plugin and starts associated resources if needed.
func EnablePlugin(reflowBasePath, pluginName string) error {
	util.Log.Infof("Enabling plugin '%s'...", pluginName)
	ctx := context.Background()

	globalState, err := config.LoadGlobalPluginState(reflowBasePath)
	if err != nil {
		return fmt.Errorf("failed to load global plugin state: %w", err)
	}

	pluginConf, exists := globalState.InstalledPlugins[pluginName]
	if !exists {
		return fmt.Errorf("plugin '%s' not found", pluginName)
	}

	if pluginConf.Enabled {
		util.Log.Infof("Plugin '%s' is already enabled.", pluginName)
		return nil
	}

	currentConfigValues, loadErr := config.LoadPluginInstanceConfig(pluginConf.ConfigPath)
	if loadErr != nil {
		util.Log.Warnf("Failed to load current config values from '%s': %v. Proceeding with potentially stale values stored in global state.", pluginConf.ConfigPath, loadErr)
		currentConfigValues = pluginConf.ConfigValues
	} else {
		util.Log.Debugf("Successfully loaded current config values from %s", pluginConf.ConfigPath)
		pluginConf.ConfigValues = currentConfigValues
	}

	// --- Actions based on plugin type ---
	if pluginConf.Type == config.PluginTypeContainer {
		util.Log.Infof("Starting resources for container plugin '%s'...", pluginName)
		metadataPath := filepath.Join(pluginConf.InstallPath, config.PluginMetadataFileName)
		metadata, parseErr := ParsePluginMetadata(metadataPath)
		if parseErr != nil {
			return fmt.Errorf("could not parse metadata for plugin '%s' during enable: %w", pluginName, parseErr)
		}
		pluginConf.Metadata = metadata

		containerID, startErr := startPluginContainer(ctx, reflowBasePath, pluginConf, currentConfigValues)
		if startErr != nil {
			return fmt.Errorf("failed to start container for plugin '%s' on enable: %w", pluginName, startErr)
		}
		pluginConf.ContainerID = containerID

		if metadata.Nginx != nil {
			nginxErr := configurePluginNginx(ctx, reflowBasePath, pluginConf)
			if nginxErr != nil {
				util.Log.Errorf("Failed to configure Nginx for plugin '%s' on enable: %v", pluginName, nginxErr)
				util.Log.Warnf("Attempting rollback: stopping container %s...", containerID[:12])
				_ = docker.StopContainer(ctx, containerID, nil)
				pluginConf.NginxConfigOk = false
			} else {
				pluginConf.NginxConfigOk = true
			}
		}
	}

	// --- Update State ---
	pluginConf.Enabled = true
	globalState.InstalledPlugins[pluginName] = pluginConf

	if err := config.SaveGlobalPluginState(reflowBasePath, globalState); err != nil {
		return fmt.Errorf("failed to save plugin state after enabling '%s': %w", pluginName, err)
	}

	util.Log.Infof("✅ Plugin '%s' enabled successfully.", pluginName)
	if pluginConf.Type == config.PluginTypeContainer && pluginConf.NginxConfigOk {
		domain, domainErr := GetEffectivePluginDomainFromConfig(reflowBasePath, pluginConf)
		if domainErr == nil {
			util.Log.Infof("   Access URL: %s", domain)
		}
	}
	return nil
}

// DisablePlugin disables a plugin and stops associated resources if needed.
func DisablePlugin(reflowBasePath, pluginName string) error {
	util.Log.Infof("Disabling plugin '%s'...", pluginName)
	ctx := context.Background()

	globalState, err := config.LoadGlobalPluginState(reflowBasePath)
	if err != nil {
		return fmt.Errorf("failed to load global plugin state: %w", err)
	}

	pluginConf, exists := globalState.InstalledPlugins[pluginName]
	if !exists {
		return fmt.Errorf("plugin '%s' not found", pluginName)
	}

	if !pluginConf.Enabled {
		util.Log.Infof("Plugin '%s' is already disabled.", pluginName)
		return nil
	}

	// --- Actions based on plugin type ---
	if pluginConf.Type == config.PluginTypeContainer {
		util.Log.Infof("Stopping resources for container plugin '%s'...", pluginName)

		// Stop Container (reuse existing function)
		if pluginConf.ContainerID != "" {
			if stopErr := stopPluginContainer(ctx, reflowBasePath, pluginConf); stopErr != nil {
				// Log error but continue disabling attempt
				util.Log.Errorf("Failed to stop container %s during disable: %v. Continuing.", pluginConf.ContainerID[:min(12, len(pluginConf.ContainerID))], stopErr)
			} else {
				// Optionally clear ContainerID on successful stop? Or keep it for re-enabling? Keep it for now.
				// pluginConf.ContainerID = ""
			}
		}

		// Remove Nginx Config (reuse existing function, if applicable)
		// Only remove if it was marked OK, otherwise it might already be gone or never existed
		if pluginConf.NginxConfigOk {
			if nginxErr := RemovePluginNginx(ctx, reflowBasePath, pluginConf); nginxErr != nil {
				// Log error but continue disabling attempt
				util.Log.Errorf("Failed to remove Nginx config for plugin '%s' during disable: %v. Continuing.", pluginName, nginxErr)
				// Should we mark NginxConfigOk as false? Yes.
				pluginConf.NginxConfigOk = false
			} else {
				pluginConf.NginxConfigOk = false // Mark as not configured after removal
			}
		}
	}

	// --- Update State ---
	pluginConf.Enabled = false
	globalState.InstalledPlugins[pluginName] = pluginConf

	if err := config.SaveGlobalPluginState(reflowBasePath, globalState); err != nil {
		// This is problematic, state doesn't reflect reality
		return fmt.Errorf("failed to save plugin state after disabling '%s': %w", pluginName, err)
	}

	util.Log.Infof("✅ Plugin '%s' disabled successfully.", pluginName)
	return nil
}
