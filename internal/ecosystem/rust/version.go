package rust

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

var (
	versionLine = regexp.MustCompile(`(?m)^\s*version\s*=\s*"[^"]*"\s*$`)
	quotedValue = regexp.MustCompile(`"[^"]*"`)
)

// SetVersion rewrites the version line of the [package] table. A package that
// inherits its version (version.workspace = true) has nothing to write here:
// the workspace manifest owns that value and SyncVersions updates it once.
func (driver) SetVersion(root string, pkg ecosystem.Package, next string) error {
	if pkg.Inherited {
		return nil
	}
	path := filepath.Join(fsx.Join(root, pkg.Path), "Cargo.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	inPackage := false
	replaced := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inPackage = trimmed == "[package]"
			continue
		}
		if !inPackage || !versionLine.MatchString(line) {
			continue
		}
		lines[i] = quotedValue.ReplaceAllStringFunc(line, func(string) string {
			return strconv.Quote(next)
		})
		replaced++
	}
	if replaced != 1 {
		return fmt.Errorf("%s: expected one [package] version line, replaced %d", path, replaced)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
