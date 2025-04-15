package config

import "time"

// GlobalConfig represents the structure of the global reflow/config.yaml
type GlobalConfig struct {
	DefaultDomain string `mapstructure:"defaultDomain" yaml:"defaultDomain"`
	Debug         bool   `mapstructure:"debug"         yaml:"debug"`
}

// ProjectEnvConfig represents environment-specific settings within a project
type ProjectEnvConfig struct {
	Domain  string `mapstructure:"domain"  yaml:"domain,omitempty"`
	EnvFile string `mapstructure:"envFile" yaml:"envFile,omitempty"`
}

// ProjectConfig represents the structure of reflow/apps/<project>/config.yaml
type ProjectConfig struct {
	ProjectName  string                      `mapstructure:"projectName" yaml:"projectName"`
	GithubRepo   string                      `mapstructure:"githubRepo"  yaml:"githubRepo"`
	AppPort      int                         `mapstructure:"appPort"     yaml:"appPort"`
	NodeVersion  string                      `mapstructure:"nodeVersion" yaml:"nodeVersion"`
	Environments map[string]ProjectEnvConfig `mapstructure:"environments" yaml:"environments"`

	// These are populated from flags if provided during 'create', not saved by default
	// but used for domain calculation if Environments.Test/Prod.Domain are empty.
	TestDomainOverride string `mapstructure:"-" yaml:"-"`
	ProdDomainOverride string `mapstructure:"-" yaml:"-"`
}

// CreateProjectArgs holds parameters for creating a new project.
type CreateProjectArgs struct {
	ProjectName string `json:"projectName" yaml:"projectName"`
	RepoURL     string `json:"repoUrl" yaml:"repoUrl"`
	AppPort     int    `json:"appPort,omitempty" yaml:"appPort,omitempty"`
	NodeVersion string `json:"nodeVersion,omitempty" yaml:"nodeVersion,omitempty"`
	TestDomain  string `json:"testDomain,omitempty" yaml:"testDomain,omitempty"`
	ProdDomain  string `json:"prodDomain,omitempty" yaml:"prodDomain,omitempty"`
	TestEnvFile string `json:"testEnvFile,omitempty" yaml:"testEnvFile,omitempty"`
	ProdEnvFile string `json:"prodEnvFile,omitempty" yaml:"prodEnvFile,omitempty"`
}

// EnvironmentState State tracks the deployment status per environment for a project
type EnvironmentState struct {
	ActiveSlot    string `json:"activeSlot"`    // "blue" or "green"
	ActiveCommit  string `json:"activeCommit"`  // Git commit hash currently active
	InactiveSlot  string `json:"inactiveSlot"`  // The other slot
	PendingCommit string `json:"pendingCommit"` // Commit deployed but not yet made active (used during deployment)
}

// ProjectState represents the structure of reflow/apps/<project>/state.json
type ProjectState struct {
	Test EnvironmentState `json:"test"`
	Prod EnvironmentState `json:"prod"`
}

// MergedProjectConfig holds both project config and state, useful for operations
type MergedProjectConfig struct {
	Config   ProjectConfig
	State    ProjectState
	BasePath string // Path to the project's directory (e.g., reflow/apps/my-app)
}

// DeploymentEvent represents a logged deployment or approval action.
type DeploymentEvent struct {
	Timestamp    time.Time `json:"timestamp"` // Time the event was logged (usually end of action)
	EventType    string    `json:"eventType"` // "deploy" or "approve"
	ProjectName  string    `json:"projectName"`
	Environment  string    `json:"environment"`            // "test" or "prod"
	CommitSHA    string    `json:"commitSHA"`              // Full commit hash involved
	Outcome      string    `json:"outcome"`                // "started", "success", "failure"
	ErrorMessage string    `json:"errorMessage,omitempty"` // Details on failure
	DurationMs   int64     `json:"durationMs,omitempty"`   // How long the action took (for success/failure events)
	TriggeredBy  string    `json:"triggeredBy,omitempty"`  // How it was triggered (e.g., "cli", "api", "user:xyz" - future enhancement)
}

// PluginType defines the kind of plugin.
type PluginType string

const (
	PluginTypeCLI       PluginType = "cli"
	PluginTypeContainer PluginType = "container"
)

// PluginSetupPrompt defines a configuration prompt for plugin setup.
type PluginSetupPrompt struct {
	Key         string `yaml:"key"`                   // Internal key to store the value (e.g., "domain", "api_key")
	Prompt      string `yaml:"prompt"`                // User-facing question (e.g., "Enter the desired domain for the dashboard:")
	Default     string `yaml:"default,omitempty"`     // Optional default value
	Required    bool   `yaml:"required,omitempty"`    // If true, user must provide a value
	Description string `yaml:"description,omitempty"` // Optional help text
}

// PluginNginxConfig defines how Nginx should be configured for a container plugin.
type PluginNginxConfig struct {
	// If true, Reflow will use a default template based on project conventions.
	// Requires 'domain' and 'containerPort' prompts or values in config.
	UseDefaultTemplate bool `yaml:"useDefaultTemplate,omitempty"`
	// Optional: Path *relative to the plugin repo root* to a custom Nginx config template file.
	// If provided, this overrides UseDefaultTemplate. The template can access plugin config values.
	CustomTemplatePath string `yaml:"customTemplatePath,omitempty"`
	// Port the plugin container listens on internally. Required if UseDefaultTemplate is true.
	// Can be specified here or gathered via a setup prompt with key "containerPort".
	ContainerPort int `yaml:"containerPort,omitempty"`
}

// PluginMetadata defines the structure of a plugin's metadata file (e.g., reflow-plugin.yaml).
type PluginMetadata struct {
	Name        string              `yaml:"name"`                  // User-friendly name of the plugin
	Version     string              `yaml:"version"`               // Plugin version
	Description string              `yaml:"description,omitempty"` // Short description
	Type        PluginType          `yaml:"type"`                  // "cli" or "container"
	Setup       []PluginSetupPrompt `yaml:"setup,omitempty"`       // List of prompts for initial configuration
	// Optional: Defines Docker build/run settings for container plugins.
	Container *struct {
		// Optional: Path relative to plugin repo root to a Dockerfile. If omitted, assumes pre-built image.
		Dockerfile string `yaml:"dockerfile,omitempty"`
		// Required if Dockerfile is omitted: The Docker image to pull and run.
		Image string `yaml:"image,omitempty"`
		// Optional build arguments if Dockerfile is used.
		BuildArgs map[string]string `yaml:"buildArgs,omitempty"`
		// Optional: Environment variables to set in the container. Values can reference plugin config keys.
		Env map[string]string `yaml:"env,omitempty"`
	} `yaml:"container,omitempty"`
	// Optional: Nginx configuration for container plugins.
	Nginx *PluginNginxConfig `yaml:"nginx,omitempty"`
	// Optional: Defines CLI command extensions provided by the plugin.
	Commands *struct {
		// Path relative to plugin repo root to a binary or script to execute for registered commands.
		// Reflow will execute this binary with command args.
		Executable string `yaml:"executable"`
		// Map of command names (e.g., "guide") to their descriptions.
		Definitions map[string]string `yaml:"definitions"`
	} `yaml:"commands,omitempty"`
}

// PluginInstanceConfig holds the specific configuration for an installed plugin instance.
// This includes answers to setup prompts and other runtime details.
type PluginInstanceConfig struct {
	PluginName    string            `json:"pluginName"`              // Internal name (usually derived from repo)
	DisplayName   string            `json:"displayName"`             // User-friendly name from metadata
	RepoURL       string            `json:"repoUrl"`                 // Source Git repository
	Version       string            `json:"version"`                 // Installed version from metadata
	InstallPath   string            `json:"installPath"`             // Full path to the installed plugin directory
	ConfigPath    string            `json:"configPath"`              // Full path to the saved plugin config file
	Type          PluginType        `json:"type"`                    // Plugin type
	ConfigValues  map[string]string `json:"configValues"`            // User-provided values from setup prompts
	Enabled       bool              `json:"enabled"`                 // Whether the plugin is currently active
	ContainerID   string            `json:"containerId,omitempty"`   // Docker container ID if applicable
	NginxConfigOk bool              `json:"nginxConfigOk,omitempty"` // Status of Nginx config generation/reload
	InstallTime   time.Time         `json:"installTime"`             // Timestamp of installation
	Metadata      *PluginMetadata   `json:"-"`                       // Loaded metadata (transient, not saved in state)
}

// GlobalPluginState represents the state of all installed plugins.
type GlobalPluginState struct {
	InstalledPlugins map[string]*PluginInstanceConfig `json:"installedPlugins"` // Keyed by PluginName
}
