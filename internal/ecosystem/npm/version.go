package npm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

var versionLine = regexp.MustCompile(`(?m)^([ \t]*)"version"([ \t]*):([ \t]*)"([^"]*)"`)

// SetVersion rewrites the top-level version field of package.json.
func (driver) SetVersion(root string, pkg ecosystem.Package, next string) error {
	path := filepath.Join(fsx.Join(root, pkg.Path), "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	matches := versionLine.FindAllStringSubmatchIndex(text, -1)
	if len(matches) != 1 {
		return fmt.Errorf("%s: expected exactly one top-level version field, found %d", path, len(matches))
	}
	m := matches[0]
	if len(m) < 10 || m[8] < 0 || m[9] < 0 {
		return fmt.Errorf("%s: version value was not captured", path)
	}
	updated := text[:m[8]] + next + text[m[9]:]
	if updated == text {
		return fmt.Errorf("%s: version field was not updated", path)
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}
