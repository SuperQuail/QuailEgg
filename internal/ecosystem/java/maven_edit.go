package java

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Element paths reported by preflight findings. They read as the route from the
// document root to the version element a round would rewrite.
const (
	elementProjectVersion  = "project/version"
	elementRevision        = "project/properties/revision"
	elementParentVersion   = "project/parent/version"
	elementDependencyPin   = "project/dependencies/dependency/version"
	elementManagedDepPin   = "project/dependencyManagement/dependencies/dependency/version"
	dependencyPathElements = "dependencies"
	dependencyPathManaged  = "dependencyManagement"
)

// literalSpan locates the version literal inside one element. Only the text run
// is replaced, so the whitespace around it and every comment outside the
// element stay byte-identical. reason is not None when the element span holds a
// comment, CDATA section or child element: replacing the whole inner span would
// drop that content, which is why such a location is reported instead.
func literalSpan(text string, el xmlElement) (start, end int, reason ecosystem.PreflightReason) {
	if el.innerStart < 0 || el.innerEnd < el.innerStart || el.innerEnd > len(text) {
		return len(text), len(text), ecosystem.PreflightReasonNone
	}
	inner := text[el.innerStart:el.innerEnd]
	if i := strings.IndexByte(inner, '<'); i >= 0 {
		switch {
		case strings.HasPrefix(inner[i:], "<!--"):
			return 0, 0, ecosystem.PreflightReasonComment
		case strings.HasPrefix(inner[i:], "<![CDATA["):
			return 0, 0, ecosystem.PreflightReasonCDATA
		case strings.HasPrefix(inner[i:], "<?"):
			return 0, 0, ecosystem.PreflightReasonProcessing
		default:
			return 0, 0, ecosystem.PreflightReasonChild
		}
	}
	literal := strings.TrimSpace(inner)
	if literal == "" {
		// An empty span has no text run to replace: a self-closing <version/>
		// would end up as <version/>2.0.0 if a caller wrote here.
		return el.innerStart, el.innerStart, ecosystem.PreflightReasonEmpty
	}
	start = el.innerStart + strings.Index(inner, literal)
	return start, start + len(literal), ecosystem.PreflightReasonNone
}

// setLiteral replaces the version literal of one element and keeps every other
// byte of the document. It refuses an element whose span holds content the
// replacement would drop.
func setLiteral(text, file, element string, el xmlElement, value string) (string, error) {
	start, end, reason := literalSpan(text, el)
	switch reason {
	case ecosystem.PreflightReasonEmpty:
		return "", errors.New(i18n.Tf("java.err.element_empty", file, element))
	case ecosystem.PreflightReasonNone:
	default:
		return "", errors.New(i18n.Tf("java.err.element_unsafe", file, element))
	}
	return text[:start] + value + text[end:], nil
}

// mavenFilePlan is the planned state of one manifest of the round.
type mavenFilePlan struct {
	manifest string
	text     string
	writes   []textReplacement
	findings []ecosystem.PreflightFinding
	seen     map[string]bool
}

// mavenPlan collects every version write and preflight finding of one round
// without touching a file.
type mavenPlan struct {
	order []string
	files map[string]*mavenFilePlan
}

// newMavenPlan returns an empty round plan.
func newMavenPlan() *mavenPlan {
	return &mavenPlan{files: map[string]*mavenFilePlan{}}
}

// file returns the plan of manifest, reading it on first use.
func (p *mavenPlan) file(root, manifest string) (*mavenFilePlan, error) {
	if planned, ok := p.files[manifest]; ok {
		return planned, nil
	}
	data, err := os.ReadFile(fsx.Join(root, manifest))
	if err != nil {
		return nil, err
	}
	planned := &mavenFilePlan{manifest: manifest, text: string(data), seen: map[string]bool{}}
	p.files[manifest] = planned
	p.order = append(p.order, manifest)
	return planned, nil
}

// ordered returns the planned manifests in the order they were read.
func (p *mavenPlan) ordered() []*mavenFilePlan {
	out := make([]*mavenFilePlan, 0, len(p.order))
	for _, manifest := range p.order {
		out = append(out, p.files[manifest])
	}
	return out
}

// findings returns every problem of the round in manifest order.
func (p *mavenPlan) findings() []ecosystem.PreflightFinding {
	var out []ecosystem.PreflightFinding
	for _, planned := range p.ordered() {
		out = append(out, planned.findings...)
	}
	return out
}

// addFinding records one problem once, even when two planned packages reach the
// same element.
func (f *mavenFilePlan) addFinding(finding ecosystem.PreflightFinding) {
	key := "f|" + finding.Element + "|" + strconv.Itoa(int(finding.Reason)) + "|" + finding.Coordinates
	if f.seen[key] {
		return
	}
	f.seen[key] = true
	f.findings = append(f.findings, finding)
}

// addWrite records the rewrite of one version literal.
func (f *mavenFilePlan) addWrite(start, end int, value string) {
	key := "w|" + strconv.Itoa(start) + "|" + strconv.Itoa(end)
	if f.seen[key] {
		return
	}
	f.seen[key] = true
	f.writes = append(f.writes, textReplacement{start: start, end: end, value: value})
}

// addReference plans the rewrite of one internal version reference. A version
// range that still accepts the planned version is kept verbatim; a range the
// round would leave, or one the tool cannot interpret, becomes a finding so the
// round stops before writing.
func (p *mavenPlan) addReference(planned *mavenFilePlan, element string, el xmlElement, target, coordinates string) {
	start, end, reason := literalSpan(planned.text, el)
	finding := ecosystem.PreflightFinding{
		Manifest:    planned.manifest,
		Element:     element,
		Coordinates: coordinates,
		Target:      target,
	}
	if reason != ecosystem.PreflightReasonNone {
		finding.Reason = reason
		planned.addFinding(finding)
		return
	}
	current := planned.text[start:end]
	if current == "" || current == target || strings.Contains(current, "${") {
		return
	}
	ranges, isRange, err := parseMavenRange(current)
	if !isRange {
		planned.addWrite(start, end, target)
		return
	}
	finding.Range = current
	if err != nil {
		finding.Reason = ecosystem.PreflightReasonRangeUnparsable
		planned.addFinding(finding)
		return
	}
	inside, err := rangeContains(ranges, target)
	switch {
	case err != nil:
		finding.Reason = ecosystem.PreflightReasonRangeUnparsable
		planned.addFinding(finding)
	case inside:
		// The expression still covers the planned version: keep it word for
		// word instead of replacing it with a fixed version.
		return
	default:
		finding.Reason = ecosystem.PreflightReasonRangeExcluded
		planned.addFinding(finding)
	}
}

// addProjectVersion plans the rewrite of the version source of node.
func (p *mavenPlan) addProjectVersion(root string, node *mavenNode, next string) error {
	if !node.manageable {
		return errors.New(mavenReasonDetail(node))
	}
	if node.inherited || strings.TrimSpace(node.doc.Version) == "" || next == "" {
		return nil
	}
	planned, err := p.file(root, node.manifest)
	if err != nil {
		return err
	}
	element, el, err := ownVersionSite(planned.text, node, fsx.Join(root, node.manifest))
	if err != nil {
		return err
	}
	start, end, reason := literalSpan(planned.text, el)
	if reason != ecosystem.PreflightReasonNone {
		planned.addFinding(ecosystem.PreflightFinding{
			Manifest: planned.manifest, Element: element, Reason: reason, Target: next,
		})
		return nil
	}
	planned.addWrite(start, end, next)
	return nil
}

// addParentVersion plans the rewrite of the parent reference of one node.
func (p *mavenPlan) addParentVersion(root string, node *mavenNode, nextByCoord map[string]string) error {
	if node.doc.Parent == nil {
		return nil
	}
	coordinates := strings.TrimSpace(node.doc.Parent.GroupID) + ":" + strings.TrimSpace(node.doc.Parent.ArtifactID)
	target, ok := nextByCoord[coordinates]
	if !ok || target == "" {
		return nil
	}
	planned, err := p.file(root, node.manifest)
	if err != nil {
		return err
	}
	parent, ok := findElement(planned.text, "project", "parent")
	if !ok {
		return nil
	}
	versions := directChildren(planned.text, parent, "version")
	if len(versions) == 0 {
		return nil
	}
	p.addReference(planned, elementParentVersion, versions[0], target, coordinates)
	return nil
}

// addDependencyVersions plans the rewrite of the internal dependency pins under
// one dependency list.
func (p *mavenPlan) addDependencyVersions(root, manifest, element string, parentPath []string, nextByCoord map[string]string) error {
	planned, err := p.file(root, manifest)
	if err != nil {
		return err
	}
	list, ok := findElement(planned.text, parentPath...)
	if !ok {
		return nil
	}
	for _, dep := range directChildren(planned.text, list, "dependency") {
		group := firstChildText(planned.text, dep, "groupId")
		artifact := firstChildText(planned.text, dep, "artifactId")
		if group == "" || artifact == "" {
			continue
		}
		versions := directChildren(planned.text, dep, "version")
		if len(versions) == 0 {
			continue
		}
		coordinates := group + ":" + artifact
		target, ok := nextByCoord[coordinates]
		if !ok || target == "" {
			continue
		}
		p.addReference(planned, element, versions[0], target, coordinates)
	}
	return nil
}

// ownVersionSite locates the element that carries the project version of node:
// the CI-friendly revision property or the literal project version.
func ownVersionSite(text string, node *mavenNode, file string) (string, xmlElement, error) {
	if strings.TrimSpace(node.doc.Version) == "${revision}" {
		el, ok := findElement(text, "project", "properties", "revision")
		if !ok {
			return "", xmlElement{}, errors.New(i18n.Tf("java.err.revision_missing", file))
		}
		return elementRevision, el, nil
	}
	el, ok := findElement(text, "project", "version")
	if !ok {
		return "", xmlElement{}, errors.New(i18n.Tf("java.err.version_missing", file))
	}
	return elementProjectVersion, el, nil
}

// mavenRoundPlan plans every version write of one reactor round and reports the
// locations it cannot rewrite safely, without touching any file. syncPkgs
// carries every package with the version its references should point at, the
// way VersionSyncer receives them; ownPkgs carries the packages whose own
// version this round writes.
func mavenRoundPlan(root string, reactor *mavenReactor, syncPkgs, ownPkgs []ecosystem.Package) (*mavenPlan, error) {
	nextByCoord := map[string]string{}
	for _, group := range [][]ecosystem.Package{syncPkgs, ownPkgs} {
		for _, pkg := range group {
			node := reactor.nodeForDir(pkg.Path)
			if node == nil || pkg.Version == "" {
				continue
			}
			nextByCoord[node.coord()] = pkg.Version
		}
	}
	for _, pkg := range syncPkgs {
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
	plan := newMavenPlan()
	for _, pkg := range ownPkgs {
		node := reactor.nodeForDir(pkg.Path)
		if node == nil {
			continue
		}
		if err := plan.addProjectVersion(root, node, pkg.Version); err != nil {
			return nil, err
		}
	}
	for _, node := range reactor.nodes {
		if !node.manageable {
			continue
		}
		if err := plan.addParentVersion(root, node, nextByCoord); err != nil {
			return nil, err
		}
		if err := plan.addDependencyVersions(root, node.manifest, elementDependencyPin, []string{"project", dependencyPathElements}, nextByCoord); err != nil {
			return nil, err
		}
		if err := plan.addDependencyVersions(root, node.manifest, elementManagedDepPin, []string{"project", dependencyPathManaged, dependencyPathElements}, nextByCoord); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// mavenOwnPlan plans only the project version writes of one round. A configured
// package outside the repository root reactor is written by SetVersion through
// the reactor of its own directory, but the reference sync never reaches it, so
// the batch check must not report locations that write will not touch.
func mavenOwnPlan(root string, reactor *mavenReactor, pkgs []ecosystem.Package) (*mavenPlan, error) {
	plan := newMavenPlan()
	for _, pkg := range pkgs {
		node := reactor.nodeForDir(pkg.Path)
		if node == nil {
			continue
		}
		if err := plan.addProjectVersion(root, node, pkg.Version); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// forEachReactor calls fn once per reactor that owns the given Maven packages.
// A repository root pom carries every package; otherwise each package is loaded
// with the reactor of its own directory.
func forEachReactor(root string, pkgs []ecosystem.Package, fn func(*mavenReactor, []ecosystem.Package) error) error {
	if fsx.Exists(fsx.Join(root, "pom.xml")) {
		reactor, err := loadMavenReactor(root)
		if err != nil {
			return err
		}
		return fn(reactor, pkgs)
	}
	for _, pkg := range pkgs {
		reactor, err := loadMavenReactorAt(root, pkg.Path)
		if err != nil {
			return err
		}
		if err := fn(reactor, []ecosystem.Package{pkg}); err != nil {
			return err
		}
	}
	return nil
}

// ownInReactor keeps the packages of one round that belong to reactor.
func ownInReactor(reactor *mavenReactor, pkgs []ecosystem.Package) []ecosystem.Package {
	out := make([]ecosystem.Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		if reactor.nodeForDir(pkg.Path) == nil {
			continue
		}
		out = append(out, pkg)
	}
	return out
}
