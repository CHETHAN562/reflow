package docker

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflow/internal/util"
	"text/template"

	"github.com/docker/docker/api/types"
)

// Dockerfile template content
// Using multi-stage build for smaller final image
const dockerfileTemplateContent = `
# Stage 1: Build Stage
# Use the Node version specified in project config
# Define ARG before FROM so the first FROM can use it if needed
ARG NODE_VERSION={{.NodeVersion}}
# Directly use template value here
FROM node:{{.NodeVersion}} as builder

WORKDIR /app

# Copy package files and install dependencies first for layer caching
COPY package.json yarn.lock* package-lock.json* pnpm-lock.yaml* ./
RUN npm ci --omit=dev

# Copy the rest of the application code
COPY . .

# Run the build command
RUN npm run build

# Stage 2: Production Stage
# Use the SAME Node image tag as the build stage for consistency and simplicity
# Directly use the template value again, avoid ARG scoping issues for FROM
FROM node:{{.NodeVersion}} as runner

WORKDIR /app

ENV NODE_ENV production

# Copy necessary files from the builder stage
COPY --from=builder /app/package.json ./package.json
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/.next ./.next
COPY --from=builder /app/public ./public
COPY --from=builder /app/next.config.* ./

# Command to run the application
# Uses the port specified in the config directly via template
CMD ["node_modules/.bin/next", "start", "-p", "{{.AppPort}}"]
`

// DockerfileData holds data for the template
type DockerfileData struct {
	NodeVersion string
	AppPort     int
}

// GenerateDockerfileContent generates the Dockerfile content based on the provided data.
func GenerateDockerfileContent(data DockerfileData) (string, error) {
	tmpl, err := template.New("dockerfile").Parse(dockerfileTemplateContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse Dockerfile template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute Dockerfile template: %w", err)
	}
	return buf.String(), nil
}

// BuildImage builds a Docker image from a given context directory and Dockerfile path.
func BuildImage(ctx context.Context, dockerfilePath, contextPath, imageName string, buildArgs map[string]*string) error {
	cli, err := GetClient()
	if err != nil {
		return err
	}

	util.Log.Infof("Building Docker image '%s'...", imageName)
	util.Log.Debugf(" Build Context: %s", contextPath)
	util.Log.Debugf(" Dockerfile: %s", dockerfilePath)

	buildContextReader, err := createTarStream(contextPath)
	if err != nil {
		return fmt.Errorf("failed to create build context tar stream: %w", err)
	}

	options := types.ImageBuildOptions{
		Dockerfile:  filepath.Base(dockerfilePath),
		Tags:        []string{imageName},
		Remove:      true,
		ForceRemove: true,
		BuildArgs:   buildArgs,
	}

	util.Log.Info("Starting image build (this may take a while)...")
	resp, err := cli.ImageBuild(ctx, buildContextReader, options)
	if err != nil {
		util.Log.Errorf("Failed to start image build for %s: %v", imageName, err)
		return fmt.Errorf("failed to build image %s: %w", imageName, err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			util.Log.Errorf("Error closing response body: %v", err)
		} else {
			util.Log.Debugf("Closed response body successfully.")
		}
	}(resp.Body)

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		var msg map[string]interface{}
		if err := json.Unmarshal(line, &msg); err == nil {
			if stream, ok := msg["stream"].(string); ok {
				fmt.Print(stream)
			} else if errorDetail, ok := msg["errorDetail"].(map[string]interface{}); ok {
				errorMsg := "unknown build error"
				if code, ok := errorDetail["code"].(float64); ok {
					errorMsg = fmt.Sprintf("code %d: %s", int(code), errorDetail["message"])
				} else if message, ok := errorDetail["message"].(string); ok {
					errorMsg = message
				}
				util.Log.Errorf("Build error: %s", errorMsg)
				return fmt.Errorf("docker build failed: %s", errorMsg)
			} else if aux, ok := msg["aux"].(map[string]interface{}); ok {
				if id, ok := aux["ID"].(string); ok {
					util.Log.Debugf("Built image layer/result ID: %s", id)
				}
			}
		} else {
			fmt.Println(string(line))
		}
	}

	if err := scanner.Err(); err != nil {
		util.Log.Errorf("Error reading build output stream: %v", err)
		return fmt.Errorf("error reading build output: %w", err)
	}

	util.Log.Infof("Successfully built image '%s'", imageName)
	return nil
}

// createTarStream creates a tar stream from the specified directory.
func createTarStream(dir string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if path == dir {
			return nil
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer func(file *os.File) {
				err := file.Close()
				if err != nil {
					util.Log.Errorf("Error closing file %s: %v", path, err)
				} else {
					util.Log.Debugf("Closed file %s successfully.", path)
				}
			}(file)
			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed during directory walk for tar stream: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}

	return &buf, nil
}
