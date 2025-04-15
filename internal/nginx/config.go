package nginx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/util"
	"text/template"
	"time"

	dockerAPIClient "github.com/docker/docker/client"
)

const nginxReloadSignal = "HUP"

const nginxSiteTemplateContent = `
# Upstream server for {{.ProjectName}} - {{.Env}} - {{.Slot}}
# Points to the specific container for this deployment slot
upstream reflow_{{.ProjectName}}_{{.Env}}_{{.Slot}}_upstream {
    server {{.ContainerName}}:{{.AppPort}};
    # hmm.. maybe add more servers for load balancing if deploying multiple instances per slot
}

server {
    listen 80;
    listen [::]:80;
    # Listen on 443 for SSL later? We would need to impl cert management integration.
    # listen 443 ssl http2;
    # listen [::]:443 ssl http2;

    server_name {{.Domain}}; # Domain for this specific environment

    # SSL configuration (requires certs mounted into nginx container)
    # ssl_certificate /etc/nginx/ssl/live/{{.Domain}}/fullchain.pem;
    # ssl_certificate_key /etc/nginx/ssl/live/{{.Domain}}/privkey.pem;
    # include /etc/nginx/ssl/options-ssl-nginx.conf; # Common SSL settings
    # ssl_dhparam /etc/nginx/ssl/ssl-dhparams.pem;

    # Proxy requests to the upstream Node.js application
    location / {
        proxy_pass http://reflow_{{.ProjectName}}_{{.Env}}_{{.Slot}}_upstream;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
    }

    access_log /var/log/nginx/{{.ProjectName}}.{{.Env}}.access.log;
    error_log /var/log/nginx/{{.ProjectName}}.{{.Env}}.error.log;
}
`

// Template for Plugin Sites (similar but simpler upstream)
const nginxPluginTemplateContent = `
# Upstream server for Reflow Plugin: {{.PluginName}}
upstream reflow_plugin_{{.PluginName}}_upstream {
    server {{.ContainerName}}:{{.AppPort}};
}

server {
    listen 80;
    listen [::]:80;
    # listen 443 ssl http2;
    # listen [::]:443 ssl http2;

    server_name {{.Domain}}; # Domain for this specific plugin

    # SSL configuration (requires separate cert management)
    # ssl_certificate /etc/nginx/ssl/live/{{.Domain}}/fullchain.pem;
    # ssl_certificate_key /etc/nginx/ssl/live/{{.Domain}}/privkey.pem;

    location / {
        proxy_pass http://reflow_plugin_{{.PluginName}}_upstream;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
    }

    access_log /var/log/nginx/plugin.{{.PluginName}}.access.log;
    error_log /var/log/nginx/plugin.{{.PluginName}}.error.log;
}
`

// TemplateData holds the data for rendering the Nginx configuration template.
type TemplateData struct {
	ProjectName   string
	Env           string
	Slot          string
	ContainerName string
	Domain        string
	AppPort       int
}

// PluginTemplateData holds the data for rendering the Nginx configuration template for plugins.
type PluginTemplateData struct {
	PluginName    string
	ContainerName string
	Domain        string
	AppPort       int
	Config        map[string]string
}

// GenerateNginxConfig generates the Nginx configuration based on the provided data.
func GenerateNginxConfig(data TemplateData) (string, error) {
	tmpl, err := template.New("nginx-site").Parse(nginxSiteTemplateContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse nginx site template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute nginx site template: %w", err)
	}
	return buf.String(), nil
}

// GenerateNginxPluginConfig generates the Nginx configuration for a plugin using default template.
func GenerateNginxPluginConfig(data PluginTemplateData) (string, error) {
	tmpl, err := template.New("nginx-plugin").Parse(nginxPluginTemplateContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse nginx plugin template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute nginx plugin template: %w", err)
	}
	return buf.String(), nil
}

// WriteNginxConfig writes the Nginx configuration to a file.
func WriteNginxConfig(reflowBasePath, projectName, env, content string) error {
	confDir := filepath.Join(reflowBasePath, config.NginxDirName, config.NginxConfDirName)
	if err := os.MkdirAll(confDir, 0755); err != nil {
		return fmt.Errorf("failed to ensure nginx conf dir %s exists: %w", confDir, err)
	}
	confFileName := fmt.Sprintf("%s.%s.conf", projectName, env)
	confFilePath := filepath.Join(confDir, confFileName)

	util.Log.Debugf("Writing Nginx config to: %s", confFilePath)
	err := os.WriteFile(confFilePath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("failed to write nginx config file %s: %w", confFilePath, err)
	}
	util.Log.Infof("Updated Nginx config file: %s", confFilePath)
	return nil
}

// WriteNginxPluginConfig writes the Nginx configuration to a file for a plugin.
func WriteNginxPluginConfig(reflowBasePath, confFileName, content string) error {
	confDir := filepath.Join(reflowBasePath, config.NginxDirName, config.NginxConfDirName)
	if err := os.MkdirAll(confDir, 0755); err != nil {
		return fmt.Errorf("failed to ensure nginx conf dir %s exists: %w", confDir, err)
	}
	// Use the provided filename (e.g., plugin.myplugin.conf)
	confFilePath := filepath.Join(confDir, confFileName)

	util.Log.Debugf("Writing Nginx plugin config to: %s", confFilePath)
	err := os.WriteFile(confFilePath, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("failed to write nginx plugin config file %s: %w", confFilePath, err)
	}
	util.Log.Infof("Updated Nginx plugin config file: %s", confFilePath)
	return nil
}

// ReloadNginx sends a SIGHUP signal to the running reflow-nginx container.
func ReloadNginx(ctx context.Context) error {
	cli, err := docker.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get docker client for nginx reload: %w", err)
	}

	util.Log.Infof("Reloading Nginx configuration...")

	containerName := config.ReflowNginxContainerName

	inspectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	inspect, err := cli.ContainerInspect(inspectCtx, containerName)
	if err != nil {
		if dockerAPIClient.IsErrNotFound(err) || errors.Is(inspectCtx.Err(), context.DeadlineExceeded) {
			util.Log.Errorf("Nginx container '%s' not found or inspect timed out, cannot reload.", containerName)
			return fmt.Errorf("nginx container '%s' not found or did not respond quickly", containerName)
		}
		return fmt.Errorf("failed to inspect nginx container '%s': %w", containerName, err)
	}
	if !inspect.State.Running {
		util.Log.Errorf("Nginx container '%s' is not running, cannot reload.", containerName)
		return fmt.Errorf("nginx container '%s' is not running", containerName)
	}

	// Short kill timeout
	killCtx, killCancel := context.WithTimeout(ctx, 5*time.Second)
	defer killCancel()

	err = cli.ContainerKill(killCtx, containerName, nginxReloadSignal)
	if err != nil {
		if errors.Is(killCtx.Err(), context.DeadlineExceeded) {
			util.Log.Errorf("Timeout sending reload signal (%s) to nginx container '%s'.", nginxReloadSignal, containerName)
			return fmt.Errorf("timeout reloading nginx: %w", err)
		}
		util.Log.Errorf("Failed to send reload signal (%s) to nginx container '%s': %v", nginxReloadSignal, containerName, err)
		return fmt.Errorf("failed to reload nginx: %w", err)
	}

	// Add a small delay to allow Nginx to process the reload
	time.Sleep(1 * time.Second)

	util.Log.Info("Nginx configuration reloaded successfully.")
	return nil
}
