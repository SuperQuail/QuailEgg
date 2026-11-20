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
// reference once the parent itself is bumped. Only the version literal changes;
// the whitespace around it, the comments next to it and every other byte of the
// document stay the same.
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
	if raw != "${revision}" && strings.Contains(raw, "${") {
		return errors.New(i18n.Tf("java.err.version_expression", raw, file))
	}
	element, el, err := ownVersionSite(text, node, file)
	if err != nil {
		return err
	}
	updated, err := setLiteral(text, file, element, el, next)
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(file, []byte(updated), 0o644)
}
