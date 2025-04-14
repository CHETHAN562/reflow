package util

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadEnvFile loads environment variables from a specified file.
func LoadEnvFile(filePath string) ([]string, error) {
	var vars []string
	if filePath == "" {
		Log.Debug("No env file path specified.")
		return vars, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			Log.Warnf("Environment file not found at %s, continuing without it.", filePath)
			return vars, nil
		}
		return nil, fmt.Errorf("failed to open env file %s: %w", filePath, err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			Log.Errorf("Error closing env file %s: %v", filePath, err)
		} else {
			Log.Debugf("Closed env file %s successfully.", filePath)
		}
	}(file)

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			Log.Warnf("Skipping invalid line %d in env file %s: Missing '='", lineNumber, filePath)
			continue
		}
		vars = append(vars, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading env file %s: %w", filePath, err)
	}
	Log.Debugf("Loaded %d variables from %s", len(vars), filePath)
	return vars, nil
}
