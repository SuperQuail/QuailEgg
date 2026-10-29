package execx

import (
	"os"
	"strings"
)

// gitHookEnvVars lists the variables git exports to hook processes. Leaking
// them into nested commands makes those commands operate on the repository
// that runs the hook instead of their own fixtures.
var gitHookEnvVars = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_PREFIX",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR",
	"GIT_NAMESPACE",
	"GIT_AUTHOR_NAME",
	"GIT_AUTHOR_EMAIL",
	"GIT_AUTHOR_DATE",
	"GIT_COMMITTER_NAME",
	"GIT_COMMITTER_EMAIL",
	"GIT_COMMITTER_DATE",
	"GIT_CONFIG_PARAMETERS",
	"GIT_CONFIG_COUNT",
}

// GitHookEnvVars returns the variable names SanitizeGitHookEnv clears.
func GitHookEnvVars() []string { return append([]string(nil), gitHookEnvVars...) }

// SanitizeGitHookEnv removes the git variables a hook inherits, so commands
// the hook spawns discover their own repository.
func SanitizeGitHookEnv() {
	for _, name := range gitHookEnvVars {
		os.Unsetenv(name)
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			os.Unsetenv(name)
		}
	}
}
