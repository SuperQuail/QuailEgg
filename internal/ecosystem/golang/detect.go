package golang

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

var moduleLine = regexp.MustCompile("(?m)^\\s*module\\s+(\\S+)")

// Detect finds the Go module at the repository root. Modules in subdirectories
// need subdirectory-prefixed release tags (sub/v1.2.3) and multi-package
// releases, which qegg does not support yet.
func (driver) Detect(root string) ([]ecosystem.Package, error) {
	path := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	match := moduleLine.FindStringSubmatch(string(data))
	if match == nil {
		return nil, fmt.Errorf("%s: missing module directive", path)
	}
	modulePath := strings.Trim(match[1], "\"")
	if modulePath == "" {
		return nil, fmt.Errorf("%s: empty module path", path)
	}
	return []ecosystem.Package{{
		Name:     modulePath,
		Path:     ".",
		Resolver: "go",
		Manifest: "go.mod",
		Publish:  true,
	}}, nil
}
