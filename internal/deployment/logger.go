package deployment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflow/internal/config"
	"reflow/internal/util"
	"sync"
)

var logMutex sync.Mutex

// getLogFilePath constructs the path to the deployment log file.
func getLogFilePath(basePath, projectName string) string {
	projectBasePath := config.GetProjectBasePath(basePath, projectName)
	return filepath.Join(projectBasePath, config.DeploymentsLogFileName)
}

// safeShortSha returns a shortened SHA string or "N/A" if the SHA is empty.
func safeShortSha(sha string) string {
	if len(sha) >= 7 {
		return sha[:7]
	} else {
		return "N/A"
	}
}

// LogEvent logs a deployment event to the project's deployment log file.
func LogEvent(basePath, projectName string, event *config.DeploymentEvent) {
	logMutex.Lock()
	defer logMutex.Unlock()

	logFilePath := getLogFilePath(basePath, projectName)

	projectDir := filepath.Dir(logFilePath)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		util.Log.Errorf("Failed to ensure project directory exists for deployment log '%s': %v", logFilePath, err)
		return
	}

	logEntryBytes, err := json.Marshal(event)
	if err != nil {
		util.Log.Errorf("Failed to marshal deployment event for project '%s': %v", projectName, err)
		return
	}
	logEntry := string(logEntryBytes) + "\n"

	file, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		util.Log.Errorf("Failed to open deployment log file '%s' for appending: %v", logFilePath, err)
		return
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			util.Log.Errorf("Failed to close deployment log file '%s': %v", logFilePath, err)
		} else {
			util.Log.Debugf("Closed deployment log file '%s'", logFilePath)
		}
	}(file)

	if _, err := file.WriteString(logEntry); err != nil {
		util.Log.Errorf("Failed to write deployment event to log file '%s': %v", logFilePath, err)
	} else {
		util.Log.Debugf("Logged deployment event to %s: Type=%s Env=%s Commit=%s Outcome=%s", logFilePath, event.EventType, event.Environment, safeShortSha(event.CommitSHA[:7]), event.Outcome)
	}
}
