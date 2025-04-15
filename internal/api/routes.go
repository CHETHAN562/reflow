package api

import (
	"net/http"

	"github.com/gorilla/mux"
)

// RegisterRoutes sets up the API endpoints and handlers.
func RegisterRoutes(router *mux.Router, basePath string) {
	apiV1 := router.PathPrefix("/api/v1").Subrouter()

	// --- Project Routes ---
	apiV1.HandleFunc("/projects", handleListProjects(basePath)).Methods(http.MethodGet)
	apiV1.HandleFunc("/projects", handleCreateProject(basePath)).Methods(http.MethodPost)
	apiV1.HandleFunc("/projects/{projectName}/status", handleGetProjectStatus(basePath)).Methods(http.MethodGet)
	apiV1.HandleFunc("/projects/{projectName}/config", handleGetProjectConfig(basePath)).Methods(http.MethodGet)
	apiV1.HandleFunc("/projects/{projectName}/config", handleUpdateProjectConfig(basePath)).Methods(http.MethodPut)
	apiV1.HandleFunc("/projects/{projectName}/{env:(?:test|prod)}/start", handleStartProjectEnv(basePath)).Methods(http.MethodPost)
	apiV1.HandleFunc("/projects/{projectName}/{env:(?:test|prod)}/stop", handleStopProjectEnv(basePath)).Methods(http.MethodPost)
	apiV1.HandleFunc("/projects/{projectName}/{env:(?:test|prod)}/logs", handleGetProjectLogs(basePath)).Methods(http.MethodGet)
	apiV1.HandleFunc("/projects/{projectName}/{env:(?:test|prod)}/envfile", handleGetEnvFile(basePath)).Methods(http.MethodGet)
	apiV1.HandleFunc("/projects/{projectName}/{env:(?:test|prod)}/envfile", handleUpdateEnvFile(basePath)).Methods(http.MethodPut)

	// --- Deployment History Route ---
	apiV1.HandleFunc("/projects/{projectName}/deployments", handleListDeployments(basePath)).Methods(http.MethodGet)

	// --- Orchestration Routes ---
	apiV1.HandleFunc("/projects/{projectName}/deploy", handleDeployProject(basePath)).Methods(http.MethodPost)
	apiV1.HandleFunc("/projects/{projectName}/approve", handleApproveProject(basePath)).Methods(http.MethodPost)

	// --- Container Routes ---
	apiV1.HandleFunc("/containers", handleListContainers()).Methods(http.MethodGet)
	apiV1.HandleFunc("/containers/{containerId}", handleGetContainer()).Methods(http.MethodGet)
	apiV1.HandleFunc("/containers/{containerId}/start", handleStartContainer()).Methods(http.MethodPost)
	apiV1.HandleFunc("/containers/{containerId}/stop", handleStopContainer()).Methods(http.MethodPost)
	apiV1.HandleFunc("/containers/{containerId}/restart", handleRestartContainer()).Methods(http.MethodPost)
	apiV1.HandleFunc("/containers/{containerId}", handleDeleteContainer()).Methods(http.MethodDelete)

	// TODO: Add routes for plugin management?
	// e.g., GET /api/v1/plugins, GET /api/v1/plugins/{pluginName}/status, POST /api/v1/plugins/{pluginName}/enable etc.
}
