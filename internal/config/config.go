package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
	"reflow/internal/util"
)

var loadedGlobalConfig *GlobalConfig

// LoadGlobalConfig loads the global configuration from the specified base path.
func LoadGlobalConfig(basePath string) (*GlobalConfig, error) {
	if loadedGlobalConfig != nil {
		return loadedGlobalConfig, nil
	}

	configFilePath := filepath.Join(basePath, GlobalConfigFileName)
	v := viper.New()
	v.SetConfigFile(configFilePath)
	v.SetConfigType("yaml")

	v.SetDefault("defaultDomain", "localhost")
	v.SetDefault("debug", false)

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			return nil, fmt.Errorf("failed to read global config file %s: %w", configFilePath, err)
		}
		util.Log.Warnf("Global config file not found at %s, using defaults.", configFilePath)
	}

	var config GlobalConfig
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal global config: %w", err)
	}

	loadedGlobalConfig = &config
	util.Log.Debugf("Loaded global config from %s", configFilePath)
	return &config, nil
}

// GetProjectBasePath returns the path to a specific project's directory.
func GetProjectBasePath(reflowBasePath, projectName string) string {
	return filepath.Join(reflowBasePath, AppsDirName, projectName)
}

// LoadProjectConfig loads a specific project's configuration.
func LoadProjectConfig(reflowBasePath, projectName string) (*ProjectConfig, error) {
	projectBasePath := GetProjectBasePath(reflowBasePath, projectName)
	configFilePath := filepath.Join(projectBasePath, ProjectConfigFileName)

	v := viper.New()
	v.SetConfigFile(configFilePath)
	v.SetConfigType("yaml")

	// Set defaults for project config (read during 'create' usually)
	// It's better to ensure these are set when the file is created.
	// v.SetDefault("appPort", 3000)
	// v.SetDefault("nodeVersion", "18-alpine")
	// ... etc ...

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) {
			return nil, fmt.Errorf("project '%s' config file not found at %s (run 'reflow project create'?)", projectName, configFilePath)
		}
		return nil, fmt.Errorf("failed to read project config file %s: %w", configFilePath, err)
	}

	var config ProjectConfig
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal project '%s' config: %w", projectName, err)
	}
	config.ProjectName = projectName

	util.Log.Debugf("Loaded project config for '%s' from %s", projectName, configFilePath)
	return &config, nil
}

// SaveProjectConfig saves the project configuration file.
func SaveProjectConfig(reflowBasePath string, projConfig *ProjectConfig) error {
	projectBasePath := GetProjectBasePath(reflowBasePath, projConfig.ProjectName)
	configFilePath := filepath.Join(projectBasePath, ProjectConfigFileName)

	data, err := yaml.Marshal(projConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal project config for '%s': %w", projConfig.ProjectName, err)
	}

	if err := os.MkdirAll(projectBasePath, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", projectBasePath, err)
	}

	if err := os.WriteFile(configFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write project config file %s: %w", configFilePath, err)
	}
	util.Log.Debugf("Saved project config for '%s' to %s", projConfig.ProjectName, configFilePath)
	return nil
}

// LoadProjectState loads the state file for a specific project.
func LoadProjectState(reflowBasePath, projectName string) (*ProjectState, error) {
	projectBasePath := GetProjectBasePath(reflowBasePath, projectName)
	stateFilePath := filepath.Join(projectBasePath, ProjectStateFileName)

	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			util.Log.Debugf("Project state file not found for '%s' at %s, returning empty state.", projectName, stateFilePath)
			return &ProjectState{}, nil
		}
		return nil, fmt.Errorf("failed to read project state file %s: %w", stateFilePath, err)
	}

	var state ProjectState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal project state file %s: %w", stateFilePath, err)
	}

	util.Log.Debugf("Loaded project state for '%s' from %s", projectName, stateFilePath)
	return &state, nil
}

// SaveProjectState saves the state file for a specific project.
func SaveProjectState(reflowBasePath, projectName string, state *ProjectState) error {
	projectBasePath := GetProjectBasePath(reflowBasePath, projectName)
	stateFilePath := filepath.Join(projectBasePath, ProjectStateFileName)

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal project state for '%s': %w", projectName, err)
	}

	if err := os.MkdirAll(projectBasePath, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", projectBasePath, err)
	}

	if err := os.WriteFile(stateFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write project state file %s: %w", stateFilePath, err)
	}
	util.Log.Debugf("Saved project state for '%s' to %s", projectName, stateFilePath)
	return nil
}

// GetEffectiveDomain calculates the domain name for a project environment.
func GetEffectiveDomain(globalCfg *GlobalConfig, projCfg *ProjectConfig, env string) (string, error) {
	var envCfg ProjectEnvConfig
	var ok bool
	var override string

	if strings.ToLower(env) == "test" {
		envCfg, ok = projCfg.Environments["test"]
		override = projCfg.TestDomainOverride
	} else if strings.ToLower(env) == "prod" {
		envCfg, ok = projCfg.Environments["prod"]
		override = projCfg.ProdDomainOverride
	} else {
		return "", fmt.Errorf("invalid environment specified: %s", env)
	}

	if !ok {
		return "", fmt.Errorf("environment '%s' not defined in project config for '%s'", env, projCfg.ProjectName)
	}

	// Priority:
	// 1. Command-line override (--test-domain / --prod-domain during create/update)
	// 2. Explicit domain set in project config environments.<env>.domain
	// 3. Calculated default domain (<project>-<env>.<global_default_domain>)

	if override != "" {
		util.Log.Debugf("Using command-line override domain for %s/%s: %s", projCfg.ProjectName, env, override)
		return override, nil
	}

	if envCfg.Domain != "" {
		util.Log.Debugf("Using configured domain for %s/%s: %s", projCfg.ProjectName, env, envCfg.Domain)
		return envCfg.Domain, nil
	}

	if globalCfg.DefaultDomain == "" {
		return "", fmt.Errorf("cannot calculate default domain for %s/%s: global defaultDomain is not set and no specific domain provided", projCfg.ProjectName, env)
	}

	calculatedDomain := fmt.Sprintf("%s-%s.%s", projCfg.ProjectName, strings.ToLower(env), globalCfg.DefaultDomain)
	util.Log.Debugf("Using calculated default domain for %s/%s: %s", projCfg.ProjectName, env, calculatedDomain)
	return calculatedDomain, nil
}
