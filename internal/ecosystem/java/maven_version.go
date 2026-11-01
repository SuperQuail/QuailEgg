package java

import (
	"errors"
	"os"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// mavenSetVersion rewrites the project version of one Maven package. A module
// that inherits the parent version is skipped: SyncVersions updates the parent
// reference once the parent itself is bumped.
func mavenSetVersion(root string, pkg ecosystem.Package, next string) error {
	reactor, err := loadReactorForPath(root, pkg.Path)
	if err != nil {
		return err
	}
	node := reactor.nodeForDir(pkg.Path)
	if node == nil {
		return errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, fsx.Join(root, pkg.Path)))
	}
	if !node.manageable {
		return errors.New(mavenReasonDetail(node))
	}
	if node.inherited || strings.TrimSpace(node.doc.Version) == "" {
		return nil
	}
	file := fsx.Join(root, node.manifest)
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	text := string(data)
	raw := strings.TrimSpace(node.doc.Version)
	switch {
	case raw == "${revision}":
		el, ok := findElement(text, "project", "properties", "revision")
		if !ok {
			return errors.New(i18n.Tf("java.err.revision_missing", file))
		}
		text = setElementText(text, el, next)
	case strings.Contains(raw, "${"):
		return errors.New(i18n.Tf("java.err.version_expression", raw, file))
	default:
		el, ok := findElement(text, "project", "version")
		if !ok {
			return errors.New(i18n.Tf("java.err.version_missing", file))
		}
		text = setElementText(text, el, next)
	}
	return fsx.WriteFileAtomic(file, []byte(text), 0o644)
}
