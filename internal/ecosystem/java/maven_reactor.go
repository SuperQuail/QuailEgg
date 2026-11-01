package java

import (
	"errors"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Maven scan reasons used by ScanManifest.
const (
	mavenReasonExternalParentVersion  = "external_parent_version"
	mavenReasonUnsupportedVersionExpr = "unsupported_version_expression"
)

// mavenNode is one project (root or module) of a Maven reactor.
type mavenNode struct {
	dir         string
	manifest    string
	doc         *pom
	parent      *mavenNode
	groupID     string
	artifactID  string
	version     string
	versionExpr string
	inherited   bool
	publish     bool
	manageable  bool
	reason      string
	deps        []string
}

// coord returns the Maven coordinate of the node.
func (n *mavenNode) coord() string {
	return strings.TrimSpace(n.groupID) + ":" + strings.TrimSpace(n.artifactID)
}

// mavenReactor is the project graph rooted at one repository root.
type mavenReactor struct {
	root    string
	nodes   []*mavenNode
	byDir   map[string]*mavenNode
	byCoord map[string][]*mavenNode
}

// loadMavenReactor loads the reactor rooted at root (a repository root).
func loadMavenReactor(root string) (*mavenReactor, error) {
	return loadMavenReactorAt(root, ".")
}

// loadMavenReactorAt loads a reactor whose root pom is at root/startDir.
func loadMavenReactorAt(root, startDir string) (*mavenReactor, error) {
	startDir = path.Clean(filepath.ToSlash(strings.TrimSpace(startDir)))
	if startDir == "" {
		startDir = "."
	}
	reactor := &mavenReactor{root: root, byDir: map[string]*mavenNode{}, byCoord: map[string][]*mavenNode{}}
	visited := map[string]bool{}
	var load func(dir string) (*mavenNode, error)
	load = func(dir string) (*mavenNode, error) {
		dir = path.Clean(dir)
		if visited[dir] {
			return nil, nil
		}
		visited[dir] = true
		manifest := path.Join(dir, "pom.xml")
		doc, err := readPom(fsx.Join(root, manifest))
		if err != nil {
			return nil, err
		}
		node := &mavenNode{dir: dir, manifest: manifest, doc: doc}
		reactor.nodes = append(reactor.nodes, node)
		reactor.byDir[dir] = node
		for _, module := range doc.Modules {
			module = strings.TrimSpace(module)
			if module == "" || strings.Contains(module, "${") {
				continue
			}
			clean := path.Clean(filepath.ToSlash(module))
			if clean == "." || clean == ".." || path.IsAbs(clean) || strings.HasPrefix(clean, "../") {
				continue
			}
			childDir := path.Join(dir, clean)
			if !fsx.Exists(fsx.Join(root, path.Join(childDir, "pom.xml"))) {
				continue
			}
			if _, err := load(childDir); err != nil {
				continue
			}
		}
		return node, nil
	}
	if _, err := load(startDir); err != nil {
		return nil, err
	}
	reactor.resolve()
	return reactor, nil
}

// resolve computes the effective coordinates, versions and publish flags.
func (r *mavenReactor) resolve() {
	for _, node := range r.nodes {
		node.artifactID = strings.TrimSpace(node.doc.ArtifactID)
		node.groupID = strings.TrimSpace(node.doc.GroupID)
		if node.groupID == "" && node.doc.Parent != nil {
			node.groupID = strings.TrimSpace(node.doc.Parent.GroupID)
		}
		if node.artifactID != "" && node.groupID != "" {
			r.byCoord[node.coord()] = append(r.byCoord[node.coord()], node)
		}
	}
	resolving := map[*mavenNode]bool{}
	done := map[*mavenNode]bool{}
	var resolve func(*mavenNode)
	resolve = func(node *mavenNode) {
		if node == nil || done[node] {
			return
		}
		if resolving[node] {
			node.manageable = false
			node.reason = mavenReasonUnsupportedVersionExpr
			return
		}
		resolving[node] = true
		if node.doc.Parent != nil {
			node.parent = r.matchParent(node)
		}
		if node.parent != nil {
			resolve(node.parent)
		}
		props := map[string]string{}
		if node.parent != nil && node.parent.manageable {
			for key, value := range propertiesOf(node.parent.doc) {
				props[key] = value
			}
		}
		own := propertiesOf(node.doc)
		for key, value := range own {
			props[key] = value
		}
		raw := strings.TrimSpace(node.doc.Version)
		node.versionExpr = raw
		switch {
		case raw == "":
			if node.parent != nil && node.parent.manageable && node.parent.version != "" {
				node.version = node.parent.version
				node.inherited = true
				node.manageable = true
			} else {
				node.manageable = false
				node.reason = mavenReasonExternalParentVersion
			}
		case strings.Contains(raw, "${"):
			if raw != "${revision}" {
				node.manageable = false
				node.reason = mavenReasonUnsupportedVersionExpr
				break
			}
			value, ok := props["revision"]
			if !ok || strings.TrimSpace(value) == "" {
				node.manageable = false
				node.reason = mavenReasonUnsupportedVersionExpr
				break
			}
			node.version = strings.TrimSpace(value)
			if _, ownHas := own["revision"]; !ownHas {
				node.inherited = true
			}
			node.manageable = true
		default:
			node.version = raw
			node.manageable = true
		}
		if node.manageable && node.artifactID == "" {
			node.manageable = false
			node.reason = mavenReasonUnsupportedVersionExpr
		}
		if node.manageable {
			ownURL := ""
			if node.doc.DistributionManagement != nil && node.doc.DistributionManagement.Repository != nil {
				ownURL = strings.TrimSpace(node.doc.DistributionManagement.Repository.URL)
			}
			node.publish = ownURL != "" || (node.parent != nil && node.parent.manageable && node.parent.publish)
		}
		resolving[node] = false
		done[node] = true
	}
	for _, node := range r.nodes {
		resolve(node)
	}
	for _, node := range r.nodes {
		seen := map[string]bool{}
		for _, dep := range node.doc.Dependencies {
			id := strings.TrimSpace(dep.ArtifactID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			node.deps = append(node.deps, id)
		}
		if node.parent != nil && node.parent.artifactID != "" {
			if !seen[node.parent.artifactID] {
				node.deps = append(node.deps, node.parent.artifactID)
			}
		}
	}
}

// matchParent finds the reactor node a module declares as its parent.
func (r *mavenReactor) matchParent(node *mavenNode) *mavenNode {
	if node.doc.Parent == nil {
		return nil
	}
	key := strings.TrimSpace(node.doc.Parent.GroupID) + ":" + strings.TrimSpace(node.doc.Parent.ArtifactID)
	candidates := r.byCoord[key]
	if len(candidates) == 0 {
		return nil
	}
	for _, candidate := range candidates {
		if candidate == node {
			continue
		}
		if candidate.dir == "." || strings.HasPrefix(node.dir+"/", candidate.dir+"/") || node.dir == candidate.dir {
			return candidate
		}
	}
	return candidates[0]
}

// propertiesOf returns the CI-friendly properties of one pom.
func propertiesOf(doc *pom) map[string]string {
	out := map[string]string{}
	if doc == nil {
		return out
	}
	values := map[string]string{
		"revision":   doc.Properties.Revision,
		"sha1":       doc.Properties.SHA1,
		"changelist": doc.Properties.Changelist,
	}
	for key, value := range values {
		if strings.TrimSpace(value) != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

// packageForNode converts a reactor node into an ecosystem package.
func packageForNode(node *mavenNode) ecosystem.Package {
	return ecosystem.Package{
		Name:      node.artifactID,
		Path:      node.dir,
		Resolver:  "java",
		Version:   node.version,
		Inherited: node.inherited,
		Publish:   node.publish,
		Manifest:  node.manifest,
		Deps:      append([]string(nil), node.deps...),
	}
}

// packages returns every manageable reactor member.
func (r *mavenReactor) packages() []ecosystem.Package {
	out := []ecosystem.Package{}
	for _, node := range r.nodes {
		if !node.manageable || node.artifactID == "" {
			continue
		}
		out = append(out, packageForNode(node))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// nodeForDir returns the reactor node at dir.
func (r *mavenReactor) nodeForDir(dir string) *mavenNode {
	return r.byDir[path.Clean(filepath.ToSlash(strings.TrimSpace(dir)))]
}

// detectMaven returns the manageable packages of a repository root pom.
func detectMaven(root string) ([]ecosystem.Package, error) {
	reactor, err := loadMavenReactor(root)
	if err != nil {
		return nil, err
	}
	return reactor.packages(), nil
}

// mavenReasonDetail renders a scan/inspect detail for an unmanageable node.
func mavenReasonDetail(node *mavenNode) string {
	switch node.reason {
	case mavenReasonExternalParentVersion:
		return i18n.Tf("java.scan.external_parent_version", node.manifest)
	case mavenReasonUnsupportedVersionExpr:
		return i18n.Tf("java.scan.unsupported_version_expression", node.manifest)
	default:
		return i18n.Tf("java.scan.no_version", node.manifest)
	}
}

// mavenInspect reads one configured Maven package.
func mavenInspect(root string, pkg ecosystem.Package) (ecosystem.Package, error) {
	reactor, err := loadReactorForPath(root, pkg.Path)
	if err != nil {
		return ecosystem.Package{}, err
	}
	node := reactor.nodeForDir(pkg.Path)
	if node == nil {
		return ecosystem.Package{}, errors.New(i18n.Tf("ecosystem.err.inspect_package", pkg.Manifest))
	}
	if !node.manageable {
		return ecosystem.Package{}, errors.New(mavenReasonDetail(node))
	}
	return packageForNode(node), nil
}

// loadReactorForPath chooses the reactor that contains dir.
func loadReactorForPath(root, dir string) (*mavenReactor, error) {
	dir = path.Clean(filepath.ToSlash(strings.TrimSpace(dir)))
	if dir == "" {
		dir = "."
	}
	if fsx.Exists(filepath.Join(root, "pom.xml")) {
		reactor, err := loadMavenReactor(root)
		if err == nil {
			if _, ok := reactor.byDir[dir]; ok {
				return reactor, nil
			}
		}
	}
	return loadMavenReactorAt(root, dir)
}

// mavenScanManifest parses one pom.xml found by the bounded repository scan.
func mavenScanManifest(root string, pkg ecosystem.Package) (ecosystem.ManifestScan, error) {
	manifest := pkg.Manifest
	if manifest == "" {
		manifest = path.Join(pkg.Path, "pom.xml")
	}
	base := ecosystem.Package{Path: pkg.Path, Manifest: manifest, Resolver: pkg.Resolver}
	if dir := path.Dir(manifest); dir == "." {
		if kind, ok := rootBuildKind(root); ok && kind != buildMaven {
			return ecosystem.ManifestScan{}, nil
		}
	}
	if foreignBuildSystem(root, manifest) {
		return ecosystem.ManifestScan{
			Package: base,
			Reason:  ecosystem.ScanReasonNestedBuild,
			Detail:  i18n.Tf("java.scan.nested_build", manifest),
		}, nil
	}
	dir := path.Dir(manifest)
	ancestor := path.Dir(dir)
	for ancestor != "." && ancestor != "/" && ancestor != "" {
		if fsx.Exists(filepath.Join(root, filepath.FromSlash(ancestor), "pom.xml")) {
			return ecosystem.ManifestScan{
				Package: base,
				Reason:  ecosystem.ScanReasonNestedBuild,
				Detail:  i18n.Tf("java.scan.nested_build", manifest),
			}, nil
		}
		ancestor = path.Dir(ancestor)
	}
	reactor, err := loadMavenReactorAt(root, dir)
	if err != nil {
		return ecosystem.ManifestScan{}, err
	}
	node := reactor.nodeForDir(dir)
	if node == nil {
		return ecosystem.ManifestScan{}, nil
	}
	if !node.manageable {
		return ecosystem.ManifestScan{
			Package: base,
			Reason:  ecosystem.ScanReasonUnsupportedVersion,
			Detail:  mavenReasonDetail(node),
		}, nil
	}
	return ecosystem.ManifestScan{Package: packageForNode(node), OK: true}, nil
}
