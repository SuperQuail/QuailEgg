package npm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

// DefaultVerifyCommands runs the test and lint scripts when the repository has
// them, so qegg init writes a verification setup that matches the project.
func (driver) DefaultVerifyCommands(root string) []string {
	var commands []string
	if hasScript(root, "test") {
		commands = append(commands, "npm test")
	}
	if hasScript(root, "lint") {
		commands = append(commands, "npm run lint")
	}
	return commands
}

func hasScript(root, name string) bool {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	script, ok := pkg.Scripts[name]
	return ok && strings.TrimSpace(script) != "" && !strings.Contains(script, "no test specified")
}

var _ ecosystem.InitAdvisor = driver{}
