package nginx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflow/internal/config"
	"reflow/internal/docker"
	"reflow/internal/util"
	"text/template"

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

// TemplateData holds the data for rendering the Nginx configuration template.
type TemplateData struct {
	ProjectName   string
	Env           string
	Slot          string
	ContainerName string
	Domain        string
	AppPort       int
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

// ReloadNginx sends a SIGHUP signal to the running reflow-nginx container.
func ReloadNginx(ctx context.Context) error {
	cli, err := docker.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get docker client for nginx reload: %w", err)
	}

	util.Log.Infof("Reloading Nginx configuration...")

	containerName := config.ReflowNginxContainerName

	inspect, err := cli.ContainerInspect(ctx, containerName)
	if err != nil {
		if dockerAPIClient.IsErrNotFound(err) {
			return fmt.Errorf("nginx container '%s' not found, cannot reload", containerName)
		}
		return fmt.Errorf("failed to inspect nginx container '%s': %w", containerName, err)
	}
	if !inspect.State.Running {
		return fmt.Errorf("nginx container '%s' is not running, cannot reload", containerName)
	}

	err = cli.ContainerKill(ctx, containerName, nginxReloadSignal)
	if err != nil {
		util.Log.Errorf("Failed to send reload signal (%s) to nginx container '%s': %v", nginxReloadSignal, containerName, err)
		return fmt.Errorf("failed to reload nginx: %w", err)
	}

	util.Log.Info("Nginx configuration reloaded successfully.")
	return nil
}
