package java

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// mavenSync keeps internal Maven references consistent after the manifests
// were bumped. It rewrites <parent><version> and internal dependency pins of
// managed packages; external coordinates are never touched.
func mavenSync(root string, pkgs []ecosystem.Package) ([]string, error) {
	if len(pkgs) == 0 {
		return nil, nil
	}
	if fsx.Exists(fsx.Join(root, "pom.xml")) {
		reactor, err := loadMavenReactor(root)
		if err != nil {
			return nil, err
		}
		return mavenSyncReactor(root, reactor, pkgs)
	}
	var changed []string
	for _, pkg := range pkgs {
		reactor, err := loadMavenReactorAt(root, pkg.Path)
		if err != nil {
			return nil, err
		}
		paths, err := mavenSyncReactor(root, reactor, []ecosystem.Package{pkg})
		if err != nil {
			return nil, err
		}
		changed = append(changed, paths...)
	}
	return uniqueSorted(changed), nil
}

// mavenSyncReactor syncs one loaded reactor.
func mavenSyncReactor(root string, reactor *mavenReactor, pkgs []ecosystem.Package) ([]string, error) {
	nextByCoord := map[string]string{}
	for _, pkg := range pkgs {
		node := reactor.nodeForDir(pkg.Path)
		if node == nil {
			continue
		}
		nextByCoord[node.coord()] = pkg.Version
	}
	for _, pkg := range pkgs {
		if !pkg.Inherited {
			continue
		}
		node := reactor.nodeForDir(pkg.Path)
		if node == nil || node.parent == nil {
			continue
		}
		parentNext, ok := nextByCoord[node.parent.coord()]
		if !ok || parentNext != pkg.Version {
			return nil, errors.New(i18n.Tf("java.err.shared_scope_required", pkg.Name))
		}
	}
	var changed []string
	for _, node := range reactor.nodes {
		if !node.manageable {
			continue
		}
		file := fsx.Join(root, node.manifest)
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		text := string(data)
		replacements := []textReplacement{}
		if node.doc.Parent != nil {
			key := strings.TrimSpace(node.doc.Parent.GroupID) + ":" + strings.TrimSpace(node.doc.Parent.ArtifactID)
			if target, ok := nextByCoord[key]; ok && target != "" {
				if parent, ok := findElement(text, "project", "parent"); ok {
					if versionEls := directChildren(text, parent, "version"); len(versionEls) > 0 {
						current := elementText(text, versionEls[0])
						if current != "" && !strings.Contains(current, "${") && current != target {
							replacements = append(replacements, textReplacement{start: versionEls[0].innerStart, end: versionEls[0].innerEnd, value: target})
						}
					}
				}
			}
		}
		replacements = append(replacements, dependencyReplacements(text, nextByCoord, "project", "dependencies")...)
		replacements = append(replacements, dependencyReplacements(text, nextByCoord, "project", "dependencyManagement", "dependencies")...)
		if len(replacements) == 0 {
			continue
		}
		text = applyReplacements(text, replacements)
		if err := fsx.WriteFileAtomic(file, []byte(text), 0o644); err != nil {
			return nil, err
		}
		changed = append(changed, node.manifest)
	}
	return uniqueSorted(changed), nil
}

// dependencyReplacements collects version edits for dependencies under path.
func dependencyReplacements(text string, nextByCoord map[string]string, path ...string) []textReplacement {
	var out []textReplacement
	deps, ok := findElement(text, path...)
	if !ok {
		return out
	}
	for _, dep := range directChildren(text, deps, "dependency") {
		group := firstChildText(text, dep, "groupId")
		artifact := firstChildText(text, dep, "artifactId")
		if group == "" || artifact == "" {
			continue
		}
		versionEls := directChildren(text, dep, "version")
		if len(versionEls) == 0 {
			continue
		}
		target, ok := nextByCoord[group+":"+artifact]
		if !ok || target == "" {
			continue
		}
		current := elementText(text, versionEls[0])
		if current == "" || strings.Contains(current, "${") || current == target {
			continue
		}
		out = append(out, textReplacement{start: versionEls[0].innerStart, end: versionEls[0].innerEnd, value: target})
	}
	return out
}

// firstChildText returns the text of the first direct child called name.
func firstChildText(text string, parent xmlElement, name string) string {
	children := directChildren(text, parent, name)
	if len(children) == 0 {
		return ""
	}
	return elementText(text, children[0])
}

// textReplacement is one content replacement.
type textReplacement struct {
	start, end int
	value      string
}

// applyReplacements applies non-overlapping replacements from the end.
func applyReplacements(text string, replacements []textReplacement) string {
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	for _, replacement := range replacements {
		if replacement.start < 0 || replacement.end > len(text) || replacement.start > replacement.end {
			continue
		}
		text = text[:replacement.start] + replacement.value + text[replacement.end:]
	}
	return text
}

// uniqueSorted returns sorted unique paths.
func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = path.Clean(filepath.ToSlash(value))
		if value == "." || value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
