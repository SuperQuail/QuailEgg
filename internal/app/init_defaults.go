package app

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

func detectDefaultBranch(ctx context.Context, root string) string {
	run := execx.OSRunner{}
	if res, err := run.Run(ctx, root, "git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		name := strings.TrimSpace(res.Stdout)
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if name != "" {
			return name
		}
	}
	if res, err := run.Run(ctx, root, "git", "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		name := strings.TrimSpace(res.Stdout)
		if name != "" && name != "HEAD" {
			return name
		}
	}
	return "main"
}

// defaultScope limits the change record rule to the detected package
// directories. Configuration, docs and workflows at the repository root then
// stay out of the rule instead of demanding a record for every scaffold file.
func defaultScope(packages map[string]config.Package) []string {
	seen := map[string]bool{}
	var scope []string
	for _, pkg := range packages {
		dir := path.Clean(filepath.ToSlash(strings.TrimSpace(pkg.Path)))
		if dir == "." || dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		scope = append(scope, dir)
	}
	sort.Strings(scope)
	return scope
}

func defaultVerifyCommands(root, ecosystemID string) []string {
	switch ecosystemID {
	case "npm":
		var commands []string
		if hasScript(root, "test") {
			commands = append(commands, "npm test")
		}
		if hasScript(root, "lint") {
			commands = append(commands, "npm run lint")
		}
		return commands
	case "python":
		if _, err := os.Stat(filepath.Join(root, "tests")); err == nil {
			if execx.LookPath("uv") {
				return []string{"uv run pytest -q"}
			}
			return []string{"pytest -q"}
		}
		return nil
	case "rust":
		return []string{"cargo test"}
	case "go":
		return []string{"go test ./..."}
	default:
		return nil
	}
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
