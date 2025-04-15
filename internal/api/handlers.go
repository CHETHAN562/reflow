package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"reflow/internal/app"
	"reflow/internal/config"
	"reflow/internal/deployment"
	"reflow/internal/docker"
	"reflow/internal/orchestrator"
	"reflow/internal/project"
	"reflow/internal/util"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// --- Project Handlers ---

// handleListProjects retrieves a list of all projects.
// GET /api/v1/projects
func handleListProjects(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		summaries, err := project.ListProjects(basePath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to list projects", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, summaries)
	}
}

// handleGetProjectStatus retrieves detailed status for a specific project.
// GET /api/v1/projects/{projectName}/status
func handleGetProjectStatus(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		if projectName == "" {
			writeError(w, http.StatusBadRequest, "Project name is required")
			return
		}

		details, err := project.GetProjectDetails(context.Background(), basePath, projectName)
		if err != nil {
			errMsg := err.Error()
			if os.IsNotExist(err) || strings.Contains(errMsg, "config file not found") || strings.Contains(errMsg, "no such file or directory") {
				writeError(w, http.StatusNotFound, "Project not found", fmt.Sprintf("Project '%s' does not exist or is not initialized.", projectName))
			} else {
				writeError(w, http.StatusInternalServerError, "Failed to get project status", err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, details)
	}
}

// handleStartProjectEnv starts a specific environment for a project.
// POST /api/v1/projects/{projectName}/{env}/start
func handleStartProjectEnv(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		env := vars["env"]

		if projectName == "" || env == "" {
			writeError(w, http.StatusBadRequest, "Project name and environment are required")
			return
		}
		if env != "test" && env != "prod" {
			writeError(w, http.StatusBadRequest, "Invalid environment specified (must be 'test' or 'prod')")
			return
		}

		util.Log.Infof("API Request: Start project '%s' environment '%s'", projectName, env)
		err := app.StartProjectEnv(context.Background(), basePath, projectName, env)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to start project %s env %s", projectName, env), err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Project '%s' environment '%s' started successfully.", projectName, env)})
	}
}

// handleStopProjectEnv stops a specific environment for a project.
// POST /api/v1/projects/{projectName}/{env}/stop
func handleStopProjectEnv(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		env := vars["env"]

		if projectName == "" || env == "" {
			writeError(w, http.StatusBadRequest, "Project name and environment are required")
			return
		}
		if env != "test" && env != "prod" {
			writeError(w, http.StatusBadRequest, "Invalid environment specified (must be 'test' or 'prod')")
			return
		}

		util.Log.Infof("API Request: Stop project '%s' environment '%s'", projectName, env)
		err := app.StopProjectEnv(context.Background(), basePath, projectName, env)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to stop project %s env %s", projectName, env), err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Project '%s' environment '%s' stopped successfully.", projectName, env)})
	}
}

// --- Orchestration Handlers ---

// handleDeployProject triggers a deployment to the test environment.
// POST /api/v1/projects/{projectName}/deploy
// Optional body: {"commit": "commit-hash-or-branch"}
func handleDeployProject(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		if projectName == "" {
			writeError(w, http.StatusBadRequest, "Project name is required")
			return
		}

		var payload struct {
			Commit string `json:"commit,omitempty"`
		}
		// Allow empty body or body with commit
		if r.Body != nil && r.ContentLength > 0 {
			err := json.NewDecoder(r.Body).Decode(&payload)
			if err != nil && !errors.Is(err, errors.New("EOF")) {
				writeError(w, http.StatusBadRequest, "Invalid JSON payload", err.Error())
				return
			}
		}
		commitIsh := payload.Commit

		util.Log.Infof("API Request: Deploy project '%s' (Commit: '%s')", projectName, commitIsh)
		err := orchestrator.DeployTest(context.Background(), basePath, projectName, commitIsh)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to deploy project %s", projectName), err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Deployment initiated for project '%s'. Check logs for status.", projectName)})
	}
}

// handleApproveProject triggers promotion from test to prod.
// POST /api/v1/projects/{projectName}/approve
func handleApproveProject(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		if projectName == "" {
			writeError(w, http.StatusBadRequest, "Project name is required")
			return
		}

		util.Log.Infof("API Request: Approve project '%s' for production", projectName)
		err := orchestrator.ApproveProd(context.Background(), basePath, projectName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to approve project %s for production", projectName), err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("Approval initiated for project '%s'. Check logs for status.", projectName)})
	}
}

// handleGetProjectLogs retrieves logs for a project environment.
// GET /api/v1/projects/{projectName}/{env}/logs?tail=100
func handleGetProjectLogs(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		env := vars["env"]
		tail := r.URL.Query().Get("tail")
		if tail == "" {
			tail = "100"
		}

		if projectName == "" || env == "" {
			writeError(w, http.StatusBadRequest, "Project name and environment are required")
			return
		}
		if env != "test" && env != "prod" {
			writeError(w, http.StatusBadRequest, "Invalid environment specified (must be 'test' or 'prod')")
			return
		}

		util.Log.Debugf("API Request: Get logs for project '%s' env '%s' (Tail: %s)", projectName, env, tail)

		logContent, err := app.GetAppLogsAsString(r.Context(), basePath, projectName, env, tail)
		if err != nil {
			if strings.Contains(err.Error(), "no suitable container found") || strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, "Logs not available", err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "Failed to get logs", err.Error())
			}
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(logContent))
	}
}

// --- Container Handlers ---

// handleListContainers lists all Reflow-managed containers.
// GET /api/v1/containers
func handleListContainers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		containers, err := docker.ListManagedContainers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to list managed containers", err.Error())
			return
		}

		// We might want to map types.Container to a simpler API struct
		// For now, return the raw Docker type (might expose too much?)
		writeJSON(w, http.StatusOK, containers)
	}
}

// handleGetContainer retrieves details for a specific container.
// GET /api/v1/containers/{containerId}
func handleGetContainer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		containerID := vars["containerId"]
		if containerID == "" {
			writeError(w, http.StatusBadRequest, "Container ID is required")
			return
		}

		inspectData, err := docker.InspectContainer(r.Context(), containerID)
		if err != nil {
			if docker.IsErrNotFound(err) {
				writeError(w, http.StatusNotFound, "Container not found", err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "Failed to inspect container", err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, inspectData)
	}
}

// handleStopContainer stops a specific container.
// POST /api/v1/containers/{containerId}/stop
func handleStopContainer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		containerID := vars["containerId"]
		if containerID == "" {
			writeError(w, http.StatusBadRequest, "Container ID is required")
			return
		}

		util.Log.Infof("API Request: Stop container '%s'", containerID)
		timeout := 10 * time.Second
		err := docker.StopContainer(r.Context(), containerID, &timeout)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to stop container", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "Container stop initiated successfully."})
	}
}

// handleStartContainer starts a specific container.
// POST /api/v1/containers/{containerId}/start
func handleStartContainer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		containerID := vars["containerId"]
		if containerID == "" {
			writeError(w, http.StatusBadRequest, "Container ID is required")
			return
		}

		util.Log.Infof("API Request: Start container '%s'", containerID)
		err := docker.StartContainer(r.Context(), containerID)
		if err != nil {
			if docker.IsErrNotFound(err) || strings.Contains(err.Error(), "not found") {
				writeError(w, http.StatusNotFound, "Container not found", err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "Failed to start container", err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "Container started successfully."})
	}
}

// handleRestartContainer restarts a specific container.
// POST /api/v1/containers/{containerId}/restart
func handleRestartContainer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		containerID := vars["containerId"]
		if containerID == "" {
			writeError(w, http.StatusBadRequest, "Container ID is required")
			return
		}

		util.Log.Infof("API Request: Restart container '%s'", containerID)
		timeout := 10 * time.Second
		err := docker.RestartContainer(r.Context(), containerID, &timeout)
		if err != nil {
			if docker.IsErrNotFound(err) {
				writeError(w, http.StatusNotFound, "Container not found", err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "Failed to restart container", err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "Container restart initiated."})
	}
}

// handleDeleteContainer removes a specific (stopped) container.
// DELETE /api/v1/containers/{containerId}
func handleDeleteContainer() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		containerID := vars["containerId"]
		if containerID == "" {
			writeError(w, http.StatusBadRequest, "Container ID is required")
			return
		}

		util.Log.Infof("API Request: Delete container '%s'", containerID)
		err := docker.RemoveContainer(r.Context(), containerID)
		if err != nil {
			if docker.IsErrNotFound(err) {
				w.WriteHeader(http.StatusNoContent)
			} else if strings.Contains(err.Error(), "cannot remove running container") {
				writeError(w, http.StatusConflict, "Cannot remove container", err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "Failed to remove container", err.Error())
			}
			return
		}
		// Success
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- Project Config and Env File Handlers ---

// handleCreateProject creates a new project via the API.
// POST /api/v1/projects
func handleCreateProject(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var args config.CreateProjectArgs
		err := json.NewDecoder(r.Body).Decode(&args)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON payload", err.Error())
			return
		}

		if args.ProjectName == "" || args.RepoURL == "" {
			writeError(w, http.StatusBadRequest, "Missing required fields: projectName and repoUrl")
			return
		}

		util.Log.Infof("API Request: Create project '%s' from repo '%s'", args.ProjectName, args.RepoURL)

		err = project.CreateProject(basePath, args)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				writeError(w, http.StatusConflict, "Project creation failed", err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "Project creation failed", err.Error())
			}
			return
		}

		w.Header().Set("Location", fmt.Sprintf("/api/v1/projects/%s/status", args.ProjectName))
		writeJSON(w, http.StatusCreated, map[string]string{"message": fmt.Sprintf("Project '%s' created successfully.", args.ProjectName)})
	}
}

// handleGetProjectConfig gets the project configuration.
// GET /api/v1/projects/{projectName}/config
func handleGetProjectConfig(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		if projectName == "" {
			writeError(w, http.StatusBadRequest, "Project name is required")
			return
		}

		projCfg, err := config.LoadProjectConfig(basePath, projectName)
		if err != nil {
			if os.IsNotExist(err) || strings.Contains(err.Error(), "config file not found") {
				writeError(w, http.StatusNotFound, "Project config not found", err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "Failed to load project config", err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, projCfg)
	}
}

// handleUpdateProjectConfig updates the project configuration.
// PUT /api/v1/projects/{projectName}/config
func handleUpdateProjectConfig(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		if projectName == "" {
			writeError(w, http.StatusBadRequest, "Project name is required")
			return
		}

		var updatedCfg config.ProjectConfig
		err := json.NewDecoder(r.Body).Decode(&updatedCfg)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid JSON payload", err.Error())
			return
		}

		if updatedCfg.ProjectName == "" {
			updatedCfg.ProjectName = projectName
		} else if updatedCfg.ProjectName != projectName {
			writeError(w, http.StatusBadRequest, "Project name in payload does not match URL path")
			return
		}

		util.Log.Infof("API Request: Update config for project '%s'", projectName)

		err = config.SaveProjectConfig(basePath, &updatedCfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to save project config", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, updatedCfg)
	}
}

// getEnvFilePath helper function to find the env file path
func getEnvFilePath(basePath, projectName, env string) (string, error) {
	projCfg, err := config.LoadProjectConfig(basePath, projectName)
	if err != nil {
		return "", fmt.Errorf("could not load project config to find env file path: %w", err)
	}

	envConf, ok := projCfg.Environments[env]
	if !ok {
		return "", fmt.Errorf("environment '%s' not defined in project config", env)
	}
	if envConf.EnvFile == "" {
		return "", fmt.Errorf("no envFile specified for environment '%s' in project config", env)
	}

	repoPath := filepath.Join(config.GetProjectBasePath(basePath, projectName), config.RepoDirName)
	fullPath := filepath.Join(repoPath, envConf.EnvFile)

	cleanPath := filepath.Clean(fullPath)
	if !strings.HasPrefix(cleanPath, filepath.Clean(repoPath)+string(os.PathSeparator)) && cleanPath != filepath.Clean(repoPath) {
		return "", fmt.Errorf("invalid env file path '%s' resolves outside repo directory", envConf.EnvFile)
	}

	return cleanPath, nil
}

// handleGetEnvFile retrieves the content of a project's environment file.
// GET /api/v1/projects/{projectName}/{env}/envfile
func handleGetEnvFile(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		env := vars["env"]
		if projectName == "" || (env != "test" && env != "prod") {
			writeError(w, http.StatusBadRequest, "Project name and valid environment (test/prod) are required")
			return
		}

		envFilePath, err := getEnvFilePath(basePath, projectName, env)
		if err != nil {
			if strings.Contains(err.Error(), "project config not found") {
				writeError(w, http.StatusNotFound, "Project not found", err.Error())
			} else if strings.Contains(err.Error(), "environment not defined") || strings.Contains(err.Error(), "no envFile specified") || strings.Contains(err.Error(), "invalid env file path") {
				writeError(w, http.StatusBadRequest, "Cannot determine env file path", err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "Error getting env file path", err.Error())
			}
			return
		}

		util.Log.Debugf("API Request: Get env file content for project '%s', env '%s' from path '%s'", projectName, env, envFilePath)
		content, err := os.ReadFile(envFilePath)
		if err != nil {
			if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "Environment file not found at specified path", envFilePath)
			} else {
				writeError(w, http.StatusInternalServerError, "Failed to read environment file", err.Error())
			}
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}
}

// handleUpdateEnvFile updates the content of a project's environment file.
// PUT /api/v1/projects/{projectName}/{env}/envfile
func handleUpdateEnvFile(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		env := vars["env"]
		if projectName == "" || (env != "test" && env != "prod") {
			writeError(w, http.StatusBadRequest, "Project name and valid environment (test/prod) are required")
			return
		}

		envFilePath, err := getEnvFilePath(basePath, projectName, env)
		if err != nil {
			if strings.Contains(err.Error(), "project config not found") {
				writeError(w, http.StatusNotFound, "Project not found", err.Error())
			} else if strings.Contains(err.Error(), "environment not defined") || strings.Contains(err.Error(), "no envFile specified") || strings.Contains(err.Error(), "invalid env file path") {
				writeError(w, http.StatusBadRequest, "Cannot determine env file path", err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "Error getting env file path", err.Error())
			}
			return
		}

		bodyBytes, err := ioutil.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to read request body", err.Error())
			return
		}
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				util.Log.Errorf("Error closing request body: %v", err)
			} else {
				util.Log.Debugf("Request body closed successfully")
			}
		}(r.Body)

		util.Log.Infof("API Request: Update env file content for project '%s', env '%s' at path '%s'", projectName, env, envFilePath)

		err = os.WriteFile(envFilePath, bodyBytes, 0644)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to write environment file", err.Error())
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// --- Deployment History Handler ---

// handleListDeployments retrieves deployment history for a project.
// GET /api/v1/projects/{projectName}/deployments?limit=25&offset=0&env=&outcome=
func handleListDeployments(basePath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		projectName := vars["projectName"]
		if projectName == "" {
			writeError(w, http.StatusBadRequest, "Project name is required")
			return
		}

		limit := r.URL.Query().Get("limit")
		offset := r.URL.Query().Get("offset")
		envFilter := r.URL.Query().Get("env")
		outcomeFilter := r.URL.Query().Get("outcome")

		util.Log.Debugf("API Request: Get deployment history for project '%s' (Limit: %s, Offset: %s, Env: %s, Outcome: %s)",
			projectName, limit, offset, envFilter, outcomeFilter)

		history, err := deployment.ListHistory(basePath, projectName, limit, offset, envFilter, outcomeFilter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to retrieve deployment history", err.Error())
			return
		}

		if history == nil {
			history = []config.DeploymentEvent{}
		}

		writeJSON(w, http.StatusOK, history)
	}
}
