package deployment

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflow/internal/config"
	"reflow/internal/util"
	"sort"
	"strconv"
	"strings"
)

// ListHistory reads deployment events from the log file.
func ListHistory(basePath, projectName, limitStr, offsetStr, envFilter, outcomeFilter string) ([]config.DeploymentEvent, error) {
	logFilePath := getLogFilePath(basePath, projectName)
	util.Log.Debugf("Reading deployment history from: %s", logFilePath)

	file, err := os.Open(logFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			util.Log.Debugf("Deployment log file '%s' not found, returning empty history.", logFilePath)
			return []config.DeploymentEvent{}, nil
		}
		return nil, fmt.Errorf("failed to open deployment log file '%s': %w", logFilePath, err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			util.Log.Errorf("Failed to close file '%s': %v", logFilePath, err)
		} else {
			util.Log.Debugf("Closed deployment log file '%s'", logFilePath)
		}
	}(file)

	var allEvents []config.DeploymentEvent
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event config.DeploymentEvent
		if err := json.Unmarshal(line, &event); err != nil {
			util.Log.Warnf("Failed to parse deployment event log line %d in '%s': %v. Skipping line.", lineNumber, logFilePath, err)
			continue
		}
		allEvents = append(allEvents, event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading deployment log file '%s': %w", logFilePath, err)
	}

	sort.SliceStable(allEvents, func(i, j int) bool {
		return allEvents[i].Timestamp.After(allEvents[j].Timestamp)
	})

	var filteredEvents []config.DeploymentEvent
	for _, event := range allEvents {
		envMatch := true
		if envFilter != "" && !strings.EqualFold(event.Environment, envFilter) {
			envMatch = false
		}
		outcomeMatch := true
		if outcomeFilter != "" && !strings.EqualFold(event.Outcome, outcomeFilter) {
			outcomeMatch = false
		}

		if envMatch && outcomeMatch {
			filteredEvents = append(filteredEvents, event)
		}
	}

	totalFiltered := len(filteredEvents)
	offset := 0
	limit := 25

	if offsetStr != "" {
		off, err := strconv.Atoi(offsetStr)
		if err == nil && off >= 0 {
			offset = off
		} else {
			util.Log.Warnf("Invalid offset value '%s', using default 0.", offsetStr)
		}
	}
	if limitStr != "" {
		lim, err := strconv.Atoi(limitStr)
		if err == nil && lim > 0 {
			limit = lim
		} else {
			util.Log.Warnf("Invalid limit value '%s', using default 25.", limitStr)
		}
	}

	start := offset
	if start >= totalFiltered {
		return []config.DeploymentEvent{}, nil
	}

	end := start + limit
	if end > totalFiltered {
		end = totalFiltered
	}

	return filteredEvents[start:end], nil
}
