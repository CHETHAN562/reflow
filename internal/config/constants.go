package config

const (
	ReflowNetworkName        = "reflow-network"
	ReflowNginxContainerName = "reflow-nginx"
	NginxImage               = "nginx:stable-alpine"

	GlobalConfigFileName   = "config.yaml"
	ProjectConfigFileName  = "config.yaml"
	ProjectStateFileName   = "state.json"
	DeploymentsLogFileName = "deployments.log"
	AppsDirName            = "apps"
	NginxDirName           = "nginx"
	NginxConfDirName       = "conf.d"
	NginxLogDirName        = "logs"
	RepoDirName            = "repo"

	PluginsDirName          = "plugins"
	PluginMetadataFileName  = "reflow-plugin.yaml"
	PluginConfigDirName     = "config"
	PluginStateFileName     = "plugins.json"
	PluginDefaultConfigName = "config.json"
)
