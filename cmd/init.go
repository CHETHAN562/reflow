package cmd

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/util"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	dockerClient "github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// initCmd represents the init command
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the Reflow environment in the target directory",
	Long: `Creates the necessary configuration files, directories, Docker network,
and starts the Nginx reverse proxy container. This command should be run
once on a new VPS or in the desired base directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		basePath := GetReflowBasePath()
		util.Log.Infof("Initializing Reflow environment at: %s", basePath)

		// --- 0. Dependency Checks ---
		util.Log.Info("Checking host dependencies...")
		if _, err := exec.LookPath("git"); err != nil {
			util.Log.Errorf("Dependency check failed: 'git' command not found in PATH.")
			util.Log.Error("Reflow requires 'git' to clone project repositories.")
			util.Log.Error("Please install Git (e.g., 'sudo apt update && sudo apt install git' or 'sudo yum install git') and ensure it's available in your PATH.")
			return fmt.Errorf("'git' command not found, please install it first")
		}
		util.Log.Info("✅ Git command found.")

		// --- 1. Create Directories ---
		if err := createRequiredDirs(basePath); err != nil {
			return err
		}

		// --- 2. Create Default Global Config ---
		if err := createDefaultGlobalConfig(basePath); err != nil {
			return err
		}

		// --- 3. Initialize Docker Client ---
		util.Log.Info("Checking Docker connectivity...")
		cli, err := docker.GetClient()
		if err != nil {
			return fmt.Errorf("docker dependency check failed: %w", err)
		}
		util.Log.Info("✅ Docker daemon connectivity successful.")
		ctx := context.Background()

		// --- 4. Create Docker Network ---
		if err := createReflowNetwork(ctx, cli); err != nil {
			return err
		}

		// --- 5. Create Nginx Config Placeholder ---
		if err := createNginxDefaultConf(basePath); err != nil {
			return err
		}

		// --- 6. Setup and Start Nginx Container ---
		if err := setupNginxContainer(ctx, cli, basePath); err != nil {
			return err
		}

		util.Log.Info("✅ Reflow environment initialized successfully.")
		util.Log.Infof("   - Configuration base: %s", basePath)
		util.Log.Infof("   - Docker network '%s' created or already exists.", config.ReflowNetworkName)
		util.Log.Infof("   - Nginx container '%s' started.", config.ReflowNginxContainerName)
		util.Log.Info("You can now create projects using 'reflow project create'.")
		return nil
	},
}

func createRequiredDirs(basePath string) error {
	dirs := []string{
		filepath.Join(basePath, config.AppsDirName),
		filepath.Join(basePath, config.NginxDirName, config.NginxConfDirName),
		filepath.Join(basePath, config.NginxDirName, config.NginxLogDirName),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			if os.IsExist(err) {
				util.Log.Debugf("Directory already exists: %s", dir)
				continue
			}
			util.Log.Errorf("Failed to create directory %s: %v", dir, err)
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		util.Log.Infof("Created directory: %s", dir)
	}
	return nil
}

func createDefaultGlobalConfig(basePath string) error {
	configFilePath := filepath.Join(basePath, config.GlobalConfigFileName)
	if _, err := os.Stat(configFilePath); err == nil {
		util.Log.Warnf("Global config file already exists at %s, skipping creation.", configFilePath)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check for config file %s: %w", configFilePath, err)
	}

	defaultConfig := config.GlobalConfig{
		DefaultDomain: "yourdomain.com",
		Debug:         false,
	}

	data, err := yaml.Marshal(&defaultConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal default global config: %w", err)
	}

	if err := os.WriteFile(configFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write global config file %s: %w", configFilePath, err)
	}

	util.Log.Infof("Created default global config: %s", configFilePath)
	util.Log.Warn("Please edit 'reflow/config.yaml' to set your actual 'defaultDomain'.")
	return nil
}

func createReflowNetwork(ctx context.Context, cli *dockerClient.Client) error {
	networks, err := cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		util.Log.Errorf("Failed to list Docker networks: %v", err)
		return fmt.Errorf("failed to list Docker networks: %w", err)
	}

	for _, net := range networks {
		if net.Name == config.ReflowNetworkName {
			util.Log.Infof("Docker network '%s' already exists.", config.ReflowNetworkName)
			return nil
		}
	}

	util.Log.Infof("Creating Docker network '%s'...", config.ReflowNetworkName)
	enableIPv6 := false
	createOptions := network.CreateOptions{
		Driver:     "bridge",
		EnableIPv6: &enableIPv6,
		Attachable: true,
	}
	_, err = cli.NetworkCreate(ctx, config.ReflowNetworkName, createOptions)
	if err != nil {
		util.Log.Errorf("Failed to create Docker network '%s': %v", config.ReflowNetworkName, err)
		return fmt.Errorf("failed to create Docker network '%s': %w", config.ReflowNetworkName, err)
	}

	util.Log.Infof("Docker network '%s' created successfully.", config.ReflowNetworkName)
	return nil
}

func createNginxDefaultConf(basePath string) error {
	confDir := filepath.Join(basePath, config.NginxDirName, config.NginxConfDirName)
	defaultConfPath := filepath.Join(confDir, "00-default.conf")

	if _, err := os.Stat(defaultConfPath); err == nil {
		util.Log.Warnf("Nginx default config already exists at %s, skipping creation.", defaultConfPath)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check for nginx default config %s: %w", defaultConfPath, err)
	}

	defaultContent := `
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _; # Catch-all

    location / {
        return 404;
    }

	access_log /var/log/nginx/default.access.log;
	error_log /var/log/nginx/default.error.log;
}
`
	if err := os.WriteFile(defaultConfPath, []byte(defaultContent), 0644); err != nil {
		return fmt.Errorf("failed to write nginx default config %s: %w", defaultConfPath, err)
	}
	util.Log.Infof("Created default Nginx config: %s", defaultConfPath)
	return nil
}

func setupNginxContainer(ctx context.Context, cli *dockerClient.Client, basePath string) error {
	_, err := cli.ContainerInspect(ctx, config.ReflowNginxContainerName)
	if err == nil {
		util.Log.Warnf("Nginx container '%s' already exists. Ensuring it's running.", config.ReflowNginxContainerName)
		startOptions := container.StartOptions{}
		if startErr := cli.ContainerStart(ctx, config.ReflowNginxContainerName, startOptions); startErr != nil {
			if !strings.Contains(strings.ToLower(startErr.Error()), "is already started") && !strings.Contains(strings.ToLower(startErr.Error()), "container already running") {
				util.Log.Errorf("Failed to start existing Nginx container '%s': %v", config.ReflowNginxContainerName, startErr)
				return fmt.Errorf("failed to start existing Nginx container '%s': %w", config.ReflowNginxContainerName, startErr)
			}
			util.Log.Infof("Nginx container '%s' is already running.", config.ReflowNginxContainerName)
		} else {
			util.Log.Infof("Started existing Nginx container '%s'.", config.ReflowNginxContainerName)
		}
		return nil
	} else if !dockerClient.IsErrNotFound(err) {
		util.Log.Errorf("Failed to inspect Nginx container '%s': %v", config.ReflowNginxContainerName, err)
		return fmt.Errorf("failed to inspect Nginx container '%s': %w", config.ReflowNginxContainerName, err)
	}

	// --- Pull Nginx Image ---
	util.Log.Infof("Pulling Nginx image '%s'...", config.NginxImage)
	pullOptions := image.PullOptions{}
	reader, err := cli.ImagePull(ctx, config.NginxImage, pullOptions)
	if err != nil {
		util.Log.Errorf("Failed to pull Nginx image '%s': %v", config.NginxImage, err)
		return fmt.Errorf("failed to pull Nginx image '%s': %w", config.NginxImage, err)
	}
	defer func(reader io.ReadCloser) {
		err := reader.Close()
		if err != nil {
			util.Log.Warnf("Error closing image pull reader: %v", err)
		}
	}(reader)
	_, err = io.Copy(ioutil.Discard, reader)
	if err != nil {
		util.Log.Warnf("Error reading image pull progress (ignoring): %v", err)
	}
	util.Log.Debugf("Image pull completed for %s", config.NginxImage)

	// --- Prepare Container Configuration ---
	nginxConfDir := filepath.Join(basePath, config.NginxDirName, config.NginxConfDirName)
	nginxLogDir := filepath.Join(basePath, config.NginxDirName, config.NginxLogDirName)

	if err := os.MkdirAll(nginxConfDir, 0755); err != nil {
		return fmt.Errorf("failed to ensure nginx conf dir %s: %w", nginxConfDir, err)
	}
	if err := os.MkdirAll(nginxLogDir, 0755); err != nil {
		return fmt.Errorf("failed to ensure nginx log dir %s: %w", nginxLogDir, err)
	}

	containerConfig := &container.Config{
		Image: config.NginxImage,
		ExposedPorts: nat.PortSet{
			"80/tcp":  struct{}{},
			"443/tcp": struct{}{},
		},
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"80/tcp":  []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "80"}},
			"443/tcp": []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "443"}},
		},
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeBind,
				Source:   nginxConfDir,
				Target:   "/etc/nginx/conf.d",
				ReadOnly: true,
			},
			{
				Type:   mount.TypeBind,
				Source: nginxLogDir,
				Target: "/var/log/nginx",
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}

	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			config.ReflowNetworkName: {},
		},
	}

	// --- Create Container ---
	util.Log.Infof("Creating Nginx container '%s'...", config.ReflowNginxContainerName)
	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, nil, config.ReflowNginxContainerName)
	if err != nil {
		if strings.Contains(err.Error(), "is already in use by container") {
			util.Log.Warnf("Nginx container name '%s' conflict during creation, assuming it exists.", config.ReflowNginxContainerName)
			startOptions := container.StartOptions{}
			if startErr := cli.ContainerStart(ctx, config.ReflowNginxContainerName, startOptions); startErr != nil && !strings.Contains(strings.ToLower(startErr.Error()), "is already started") && !strings.Contains(strings.ToLower(startErr.Error()), "container already running") {
				util.Log.Errorf("Failed to start conflicting Nginx container '%s': %v", config.ReflowNginxContainerName, startErr)
				return fmt.Errorf("failed to start conflicting Nginx container '%s': %w", config.ReflowNginxContainerName, startErr)
			}
			util.Log.Infof("Ensured conflicting Nginx container '%s' is running.", config.ReflowNginxContainerName)
			return nil
		}
		util.Log.Errorf("Failed to create Nginx container '%s': %v", config.ReflowNginxContainerName, err)
		return fmt.Errorf("failed to create Nginx container '%s': %w", config.ReflowNginxContainerName, err)
	}

	// --- Start Container ---
	util.Log.Infof("Starting Nginx container '%s' (ID: %s)...", config.ReflowNginxContainerName, resp.ID[:12])
	startOptions := container.StartOptions{}
	if err := cli.ContainerStart(ctx, resp.ID, startOptions); err != nil {
		util.Log.Errorf("Failed to start Nginx container '%s': %v", config.ReflowNginxContainerName, err)
		removeOptions := container.RemoveOptions{Force: true}
		if removeErr := cli.ContainerRemove(context.Background(), resp.ID, removeOptions); removeErr != nil {
			util.Log.Warnf("Failed to remove partially created container %s after start failure: %v", resp.ID[:12], removeErr)
		}
		return fmt.Errorf("failed to start Nginx container '%s': %w", config.ReflowNginxContainerName, err)
	}

	util.Log.Infof("Nginx container '%s' started successfully.", config.ReflowNginxContainerName)
	return nil
}

func init() {
	rootCmd.AddCommand(initCmd)
}
