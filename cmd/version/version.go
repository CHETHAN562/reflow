package version

import (
	"fmt"
	"os"
	rtdebug "runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

// set at build time via ldflags.
var version = "dev"
var repository = ""

var buildInfo *rtdebug.BuildInfo

func init() {
	var ok bool
	buildInfo, ok = rtdebug.ReadBuildInfo()
	if !ok {
		fmt.Fprintln(os.Stderr, "Warning: Could not read build info via runtime/debug.")
	}
}

// AddVersionCommand defines the version command.
func AddVersionCommand(rootCmd *cobra.Command) {
	var versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the version number and repository of Reflow",
		Long:  `Displays the current version and source repository of the Reflow executable.`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Reflow version: %s\n", GetVersion())
			repo := GetRepository()
			if repo != "" {
				fmt.Printf("Source Repository: https://github.com/%s\n", repo)
			}
		},
	}
	rootCmd.AddCommand(versionCmd)
}

// GetVersion returns the embedded version string.
func GetVersion() string {
	if version == "dev" && buildInfo != nil && buildInfo.Main.Version != "" && buildInfo.Main.Version != "(devel)" {
		return buildInfo.Main.Version
	}
	return version
}

// GetRepository returns the embedded repository slug (owner/repo).
func GetRepository() string {
	if repository != "" {
		return repository
	}
	if buildInfo != nil && buildInfo.Main.Path != "" {
		pathParts := strings.Split(buildInfo.Main.Path, "/")
		if len(pathParts) >= 3 && pathParts[0] == "github.com" {
			repo := fmt.Sprintf("%s/%s", pathParts[1], pathParts[2])
			return repo
		}
	}
	if repository == "" {
		return "RevereInc/reflow"
	}
	return ""
}
