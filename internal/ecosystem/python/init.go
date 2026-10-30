package python

import (
	"os"
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// DefaultVerifyCommands runs pytest when the repository has a tests directory.
// It prefers uv when the tool is available, because uv workspaces use it too.
func (driver) DefaultVerifyCommands(root string) []string {
	if _, err := os.Stat(filepath.Join(root, "tests")); err != nil {
		return nil
	}
	if execx.LookPath("uv") {
		return []string{"uv run pytest -q"}
	}
	return []string{"pytest -q"}
}

var _ ecosystem.InitAdvisor = driver{}
