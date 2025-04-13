package config

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
