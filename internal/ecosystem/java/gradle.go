package java

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Gradle scan reason used by ScanManifest.
const gradleReasonUnsupportedVersion = "unsupported_version_source"

// gradleInclude is one included Gradle project.
type gradleInclude struct {
	projectPath string
	dir         string
	name        string
}

// gradleSettings is the project layout declared by settings.gradle(.kts).
type gradleSettings struct {
	name     string
	file     string
	includes []gradleInclude
}

// gradleAssignment is one version assignment in a Gradle build script.
type gradleAssignment struct {
	file  string
	scope string
	kind  string
	value string
	quote byte
	raw   string
	start int
	end   int
}

// Gradle assignment scopes and kinds.
const (
	gradleScopeTop         = "top"
	gradleScopeAllProjects = "allprojects"
	gradleScopeSubprojects = "subprojects"
	gradleKindLiteral      = "literal"
	gradleKindProperty     = "property"
)

// gradleProject is one Gradle project resolved from a workspace.
type gradleProject struct {
	dir         string
	name        string
	projectPath string
	buildFile   string
	version     string
	inherited   bool
	publish     bool
	reason      string
	assignment  *gradleAssignment
	propertyKey string
}

// gradleWorkspace is the repository-wide Gradle layout.
type gradleWorkspace struct {
	root      string
	settings  gradleSettings
	rootBuild string
	rootText  string
	rootCode  string
	props     map[string]string
	rootAsg   []gradleAssignment
}

var (
	gradleIncludeParen = regexp.MustCompile("(?m)^[ \\t]*include\\s*\\(([^)]*)\\)")
	gradleIncludeLine  = regexp.MustCompile("(?m)^[ \\t]*include[ \\t]+([^\\n]+)")
	gradleRootName     = regexp.MustCompile("(?m)^[ \\t]*rootProject\\.name[ \\t]*=[ \\t]*[\"']([^\"']+)[\"']")
	gradleVersionLine  = regexp.MustCompile("(?m)^[ \\t]*version\\b[ \\t]*(?:=[ \\t]*|[ \\t]+)([^\\n]*)$")
	gradleIdentifier   = regexp.MustCompile("^[A-Za-z_$][A-Za-z0-9_$.]*$")
	gradlePropertyCall = regexp.MustCompile("^(?:project\\.)?(?:property|providers\\.gradleProperty)\\s*\\(\\s*[\"']([^\"']+)[\"']\\s*\\)")
	gradleFileDef      = regexp.MustCompile("(?m)\\bdef\\s+[A-Za-z_$][A-Za-z0-9_$]*\\s*=\\s*file\\s*\\(")
	gradleHasURL       = regexp.MustCompile("(?m)\\burl\\b\\s*(?:=\\s*)?(?:uri\\s*\\(\\s*)?[\"'][^\"']+[\"']")
	gradleEmptyURL     = regexp.MustCompile("(?m)\\burl\\b\\s*(?:=\\s*)?[\"']\\s*[\"']")
)

// loadGradleWorkspace reads settings, the root build script and properties.
func loadGradleWorkspace(root string) (*gradleWorkspace, error) {
	ws := &gradleWorkspace{root: root, props: readGradleProperties(root)}
	if name, ok := gradleSettingsFile(root); ok {
		ws.settings.file = name
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		text := stripGradleComments(string(data))
		if match := gradleRootName.FindStringSubmatch(text); match != nil {
			ws.settings.name = strings.TrimSpace(match[1])
		}
		ws.settings.includes = gradleIncludes(root, text)
	}
	if ws.settings.name == "" {
		ws.settings.name = filepath.Base(root)
	}
	if name, ok := gradleBuildFile(root); ok {
		ws.rootBuild = name
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return nil, err
		}
		ws.rootText = string(data)
		ws.rootCode = stripGradleComments(ws.rootText)
		ws.rootAsg = parseGradleAssignments(ws.rootCode, name)
	}
	return ws, nil
}

// gradleIncludes parses settings includes, falling back to child directories
// that carry a build script (junit5 uses named build files).
func gradleIncludes(root, code string) []gradleInclude {
	seen := map[string]bool{}
	var out []gradleInclude
	add := func(projectPath string) {
		projectPath = strings.TrimSpace(projectPath)
		projectPath = strings.Trim(projectPath, "\"'")
		projectPath = strings.TrimPrefix(projectPath, ":")
		projectPath = strings.ReplaceAll(projectPath, ":", "/")
		if projectPath == "" || seen[projectPath] {
			return
		}
		seen[projectPath] = true
		out = append(out, gradleInclude{projectPath: ":" + projectPath, dir: projectPath, name: path.Base(projectPath)})
	}
	for _, match := range gradleIncludeParen.FindAllStringSubmatch(code, -1) {
		for _, part := range strings.Split(match[1], ",") {
			add(part)
		}
	}
	for _, match := range gradleIncludeLine.FindAllStringSubmatch(code, -1) {
		for _, part := range strings.Split(match[1], ",") {
			add(part)
		}
	}
	if len(out) > 0 {
		sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })
		return out
	}
	if strings.Contains(code, "include") {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		switch entry.Name() {
		case "build", "gradle", "src", "out", "node_modules":
			continue
		}
		dir := entry.Name()
		if _, ok := gradleBuildFile(fsx.Join(root, dir)); ok {
			add(dir)
			continue
		}
		for _, suffix := range []string{".gradle", ".gradle.kts"} {
			if fsx.Exists(filepath.Join(root, dir, dir+suffix)) {
				add(dir)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].dir < out[j].dir })
	return out
}

// stripGradleComments replaces comments with spaces while keeping offsets.
func stripGradleComments(text string) string {
	out := []byte(text)
	for i := 0; i < len(text); {
		switch {
		case i+1 < len(text) && text[i] == '/' && text[i+1] == '/':
			for i < len(text) && text[i] != '\n' {
				out[i] = ' '
				i++
			}
		case i+1 < len(text) && text[i] == '/' && text[i+1] == '*':
			out[i], out[i+1] = ' ', ' '
			i += 2
			for i < len(text) {
				if i+1 < len(text) && text[i] == '*' && text[i+1] == '/' {
					out[i], out[i+1] = ' ', ' '
					i += 2
					break
				}
				if text[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
		case text[i] == '\'' || text[i] == '"':
			quote := text[i]
			i++
			for i < len(text) {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == quote {
					i++
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return string(out)
}

// blockBodies returns the bodies of keyword { ... } blocks.
func blockBodies(code, keyword string) []string {
	pattern := regexp.MustCompile("\\b" + regexp.QuoteMeta(keyword) + "\\s*\\{")
	var out []string
	for _, loc := range pattern.FindAllStringIndex(code, -1) {
		open := loc[1] - 1
		close := matchBrace(code, open)
		if close > open {
			out = append(out, code[open+1:close])
		}
	}
	return out
}

// matchBrace returns the index of the closing brace matching open.
func matchBrace(code string, open int) int {
	depth := 0
	for i := open; i < len(code); i++ {
		switch code[i] {
		case '\'', '"':
			quote := code[i]
			i++
			for i < len(code) {
				if code[i] == '\\' {
					i += 2
					continue
				}
				if code[i] == quote {
					break
				}
				i++
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// rangesOf returns body ranges for keyword blocks.
func rangesOf(code, keyword string) [][2]int {
	pattern := regexp.MustCompile("\\b" + regexp.QuoteMeta(keyword) + "\\s*\\{")
	var out [][2]int
	for _, loc := range pattern.FindAllStringIndex(code, -1) {
		open := loc[1] - 1
		close := matchBrace(code, open)
		if close > open {
			out = append(out, [2]int{open, close})
		}
	}
	return out
}

// inRanges reports whether offset is inside one of the ranges.
func inRanges(offset int, ranges [][2]int) bool {
	for _, r := range ranges {
		if offset > r[0] && offset < r[1] {
			return true
		}
	}
	return false
}

// parseGradleAssignments finds version assignments and their scopes.
func parseGradleAssignments(code, file string) []gradleAssignment {
	all := rangesOf(code, "allprojects")
	sub := rangesOf(code, "subprojects")
	var out []gradleAssignment
	for _, loc := range gradleVersionLine.FindAllStringSubmatchIndex(code, -1) {
		if len(loc) < 4 {
			continue
		}
		raw := strings.TrimSpace(code[loc[2]:loc[3]])
		if raw == "" {
			continue
		}
		scope := gradleScopeTop
		if inRanges(loc[2], all) {
			scope = gradleScopeAllProjects
		} else if inRanges(loc[2], sub) {
			scope = gradleScopeSubprojects
		}
		valueStart, valueEnd, kind, value, quote := parseGradleValue(code, loc[2], loc[3])
		if kind == "" {
			continue
		}
		out = append(out, gradleAssignment{file: file, scope: scope, kind: kind, value: value, quote: quote, raw: raw, start: valueStart, end: valueEnd})
	}
	return out
}

// parseGradleValue classifies one version assignment value.
func parseGradleValue(code string, start, end int) (int, int, string, string, byte) {
	valueStart := start + len(code[start:end]) - len(strings.TrimLeft(code[start:end], " \t"))
	valueEnd := end
	for valueEnd > valueStart && (code[valueEnd-1] == ' ' || code[valueEnd-1] == '\t' || code[valueEnd-1] == '\r') {
		valueEnd--
	}
	raw := strings.TrimSpace(code[valueStart:valueEnd])
	if raw == "" {
		return 0, 0, "", "", 0
	}
	if raw[0] == '\'' || raw[0] == '"' {
		quote := raw[0]
		if len(raw) < 2 || raw[len(raw)-1] != quote {
			return 0, 0, "", "", 0
		}
		inner := raw[1 : len(raw)-1]
		if strings.Contains(inner, "${{") || strings.Contains(inner, "${") {
			key := strings.TrimSpace(inner)
			key = strings.TrimPrefix(key, "${{")
			key = strings.TrimSuffix(key, "}")
			key = strings.TrimPrefix(key, "${")
			if key == "" {
				return 0, 0, "", "", 0
			}
			return valueStart, valueEnd, gradleKindProperty, key, quote
		}
		return valueStart, valueEnd, gradleKindLiteral, inner, quote
	}
	if match := gradlePropertyCall.FindStringSubmatch(raw); match != nil {
		return valueStart, valueEnd, gradleKindProperty, match[1], 0
	}
	if gradleIdentifier.MatchString(raw) {
		key := strings.TrimPrefix(raw, "project.")
		return valueStart, valueEnd, gradleKindProperty, key, 0
	}
	return valueStart, valueEnd, gradleKindProperty, "", 0
}

// resolveProject resolves one project against the workspace.
func (ws *gradleWorkspace) resolveProject(dir, name, projectPath, buildFile string) gradleProject {
	project := gradleProject{dir: dir, name: name, projectPath: projectPath, buildFile: buildFile}
	own := []gradleAssignment{}
	ownCode := ""
	if buildFile != "" {
		if data, err := os.ReadFile(fsx.Join(ws.root, buildFile)); err == nil {
			ownCode = stripGradleComments(string(data))
			own = parseGradleAssignments(ownCode, buildFile)
		}
	}
	type candidate struct {
		asg       gradleAssignment
		inherited bool
	}
	var candidates []candidate
	addScope := func(assignments []gradleAssignment, inherited bool, scopes ...string) {
		for _, asg := range assignments {
			for _, scope := range scopes {
				if asg.scope == scope {
					candidates = append(candidates, candidate{asg: asg, inherited: inherited})
					return
				}
			}
		}
	}
	if dir == "." {
		addScope(own, false, gradleScopeTop)
		addScope(ws.rootAsg, false, gradleScopeTop)
		addScope(ws.rootAsg, false, gradleScopeAllProjects)
	} else {
		addScope(own, false, gradleScopeTop)
		addScope(ws.rootAsg, true, gradleScopeSubprojects)
		addScope(ws.rootAsg, true, gradleScopeAllProjects)
	}
	for _, item := range candidates {
		switch item.asg.kind {
		case gradleKindLiteral:
			project.version = item.asg.value
			project.inherited = item.inherited
			project.assignment = &item.asg
			project.publish = ws.projectPublishes(project, ownCode)
			return project
		case gradleKindProperty:
			value, ok := ws.props[item.asg.value]
			if !ok || strings.TrimSpace(value) == "" {
				project.reason = gradleReasonUnsupportedVersion
				return project
			}
			project.version = strings.TrimSpace(value)
			project.inherited = item.inherited
			project.propertyKey = item.asg.value
			project.assignment = &item.asg
			project.publish = ws.projectPublishes(project, ownCode)
			return project
		}
	}
	if value, ok := ws.props["version"]; ok && strings.TrimSpace(value) != "" {
		project.version = strings.TrimSpace(value)
		project.propertyKey = "version"
		project.inherited = dir != "."
		project.publish = ws.projectPublishes(project, ownCode)
		return project
	}
	if gradleFileDef.MatchString(ws.rootCode) || gradleFileDef.MatchString(ownCode) {
		project.reason = gradleReasonUnsupportedVersion
		return project
	}
	if dir != "." && strings.Contains(ownCode, "versionCode") && strings.Contains(ownCode, "defaultConfig") {
		project.reason = gradleReasonUnsupportedVersion
		return project
	}
	project.publish = ws.projectPublishes(project, ownCode)
	return project
}

// projectPublishes reports whether a project has a working maven-publish setup.
func (ws *gradleWorkspace) projectPublishes(project gradleProject, ownCode string) bool {
	codes := []string{ownCode}
	if project.dir == "." {
		codes = []string{gradleRootPublishingCode(ownCode)}
	} else {
		for _, body := range blockBodies(ws.rootCode, "allprojects") {
			codes = append(codes, body)
		}
		for _, body := range blockBodies(ws.rootCode, "subprojects") {
			codes = append(codes, body)
		}
	}
	for _, code := range codes {
		if code == "" {
			continue
		}
		if strings.Contains(code, "maven-publish") && hasMavenRepository(code) {
			return true
		}
	}
	return false
}

// gradleRootPublishingCode keeps the root project scope of a build script.
func gradleRootPublishingCode(code string) string {
	for _, r := range rangesOf(code, "subprojects") {
		if r[0] >= 0 && r[1] <= len(code) {
			code = code[:r[0]] + strings.Repeat(" ", r[1]-r[0]) + code[r[1]:]
		}
	}
	return code
}

// hasMavenRepository reports whether code configures a Maven repository URL.
func hasMavenRepository(code string) bool {
	for _, publishing := range blockBodies(code, "publishing") {
		for _, repositories := range blockBodies(publishing, "repositories") {
			for _, maven := range blockBodies(repositories, "maven") {
				if gradleHasURL.MatchString(maven) && !gradleEmptyURL.MatchString(maven) {
					return true
				}
			}
		}
	}
	return false
}

// detectGradle returns the manageable packages of a Gradle repository.
func detectGradle(root string) ([]ecosystem.Package, error) {
	ws, err := loadGradleWorkspace(root)
	if err != nil {
		return nil, err
	}
	rootProject := ws.resolveProject(".", ws.settings.name, ":", ws.rootBuild)
	out := []ecosystem.Package{}
	if rootProject.version != "" && rootProject.reason == "" {
		out = append(out, gradlePackage(rootProject))
	}
	for _, include := range ws.settings.includes {
		buildFile, ok := gradleBuildFileFor(fsx.Join(root, include.dir), include.name)
		if !ok {
			continue
		}
		project := ws.resolveProject(include.dir, include.name, include.projectPath, path.Join(include.dir, buildFile))
		if project.version == "" || project.reason != "" {
			continue
		}
		out = append(out, gradlePackage(project))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// gradleBuildFileFor returns the build file of one Gradle project.
func gradleBuildFileFor(dir, name string) (string, bool) {
	if file, ok := gradleBuildFile(dir); ok {
		return file, true
	}
	for _, suffix := range []string{".gradle", ".gradle.kts"} {
		if fsx.Exists(filepath.Join(dir, name+suffix)) {
			return name + suffix, true
		}
	}
	return "", false
}

// gradlePackage converts a resolved project into an ecosystem package.
func gradlePackage(project gradleProject) ecosystem.Package {
	manifest := project.buildFile
	return ecosystem.Package{
		Name:      project.name,
		Path:      project.dir,
		Resolver:  "java",
		Version:   project.version,
		Inherited: project.inherited,
		Publish:   project.publish,
		Manifest:  manifest,
	}
}

// projectFor resolves the project at dir in the workspace.
func (ws *gradleWorkspace) projectFor(root, dir string) (gradleProject, error) {
	dir = path.Clean(filepath.ToSlash(strings.TrimSpace(dir)))
	if dir == "" {
		dir = "."
	}
	buildFile, _ := gradleBuildFileFor(fsx.Join(root, dir), path.Base(dir))
	if dir == "." {
		return ws.resolveProject(".", ws.settings.name, ":", ws.rootBuild), nil
	}
	for _, include := range ws.settings.includes {
		if path.Clean(include.dir) == dir {
			return ws.resolveProject(include.dir, include.name, include.projectPath, path.Join(include.dir, buildFile)), nil
		}
	}
	if buildFile != "" {
		return ws.resolveProject(dir, path.Base(dir), ":"+strings.ReplaceAll(dir, "/", ":"), path.Join(dir, buildFile)), nil
	}
	return gradleProject{}, errors.New(i18n.Tf("ecosystem.err.inspect_manifest", dir, fsx.Join(root, dir)))
}

// gradleSetVersion rewrites the Gradle version source of one project.
func gradleSetVersion(root string, pkg ecosystem.Package, next string) error {
	ws, err := loadGradleWorkspace(root)
	if err != nil {
		return err
	}
	project, err := ws.projectFor(root, pkg.Path)
	if err != nil {
		return err
	}
	if project.inherited {
		return nil
	}
	if project.reason != "" {
		return errors.New(i18n.Tf("java.err.gradle_version_source", pkg.Name))
	}
	if project.propertyKey != "" {
		_, err := writeGradleProperty(root, project.propertyKey, next)
		return err
	}
	if project.assignment == nil || project.assignment.kind != gradleKindLiteral {
		return errors.New(i18n.Tf("java.err.gradle_version_source", pkg.Name))
	}
	file := fsx.Join(root, project.assignment.file)
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	quote := string(project.assignment.quote)
	if quote == "" {
		quote = "'"
	}
	text := string(data)
	text = text[:project.assignment.start] + quote + next + quote + text[project.assignment.end:]
	return fsx.WriteFileAtomic(file, []byte(text), 0o644)
}

// gradleVersionPaths reports the property file written by SetVersion.
func gradleVersionPaths(root string, pkg ecosystem.Package, next string) ([]string, error) {
	ws, err := loadGradleWorkspace(root)
	if err != nil {
		return nil, err
	}
	project, err := ws.projectFor(root, pkg.Path)
	if err != nil {
		return nil, err
	}
	if project.inherited {
		return nil, nil
	}
	if project.propertyKey != "" {
		return []string{"gradle.properties"}, nil
	}
	return nil, nil
}

// isIncluded reports whether dir belongs to the settings-defined workspace.
func (ws *gradleWorkspace) isIncluded(dir string) bool {
	dir = path.Clean(dir)
	for _, include := range ws.settings.includes {
		if path.Clean(include.dir) == dir {
			return true
		}
	}
	return false
}

// gradleScanManifest parses one Gradle build file found by the scan.
func gradleScanManifest(root string, pkg ecosystem.Package) (ecosystem.ManifestScan, error) {
	manifest := pkg.Manifest
	if manifest == "" {
		manifest = path.Join(pkg.Path, "build.gradle")
	}
	base := ecosystem.Package{Path: pkg.Path, Manifest: manifest, Resolver: pkg.Resolver}
	if dir := path.Dir(manifest); dir == "." {
		if kind, ok := rootBuildKind(root); ok && kind != buildGradle {
			return ecosystem.ManifestScan{}, nil
		}
	}
	if foreignBuildSystem(root, manifest) {
		return ecosystem.ManifestScan{Package: base, Reason: ecosystem.ScanReasonNestedBuild, Detail: i18n.Tf("java.scan.nested_build", manifest)}, nil
	}
	ws, err := loadGradleWorkspace(root)
	if err != nil {
		return ecosystem.ManifestScan{}, err
	}
	dir := path.Dir(manifest)
	if dir == "" {
		dir = "."
	}
	if dir != "." && !ws.isIncluded(dir) {
		return ecosystem.ManifestScan{Package: base, Reason: ecosystem.ScanReasonNestedBuild, Detail: i18n.Tf("java.scan.nested_build", manifest)}, nil
	}
	project, err := ws.projectFor(root, dir)
	if err != nil {
		return ecosystem.ManifestScan{}, err
	}
	if project.reason != "" {
		return ecosystem.ManifestScan{Package: base, Reason: ecosystem.ScanReasonUnsupportedVersion, Detail: i18n.Tf("java.scan.gradle_version_source", manifest)}, nil
	}
	if project.version == "" {
		return ecosystem.ManifestScan{Package: base, Reason: ecosystem.ScanReasonUnsupportedVersion, Detail: i18n.Tf("java.scan.no_version", manifest)}, nil
	}
	return ecosystem.ManifestScan{Package: gradlePackage(project), OK: true}, nil
}

// gradleInspect reads one configured Gradle package.
func gradleInspect(root string, pkg ecosystem.Package) (ecosystem.Package, error) {
	ws, err := loadGradleWorkspace(root)
	if err != nil {
		return ecosystem.Package{}, err
	}
	project, err := ws.projectFor(root, pkg.Path)
	if err != nil {
		return ecosystem.Package{}, err
	}
	if project.reason != "" {
		return ecosystem.Package{}, errors.New(i18n.Tf("java.scan.gradle_version_source", project.buildFile))
	}
	if project.version == "" {
		return ecosystem.Package{}, errors.New(i18n.Tf("java.scan.no_version", project.buildFile))
	}
	return gradlePackage(project), nil
}

// gradleSync writes the shared Gradle version source once.
func gradleSync(root string, pkgs []ecosystem.Package) ([]string, error) {
	ws, err := loadGradleWorkspace(root)
	if err != nil {
		return nil, err
	}
	inherited := []ecosystem.Package{}
	for _, pkg := range pkgs {
		project, err := ws.projectFor(root, pkg.Path)
		if err != nil {
			continue
		}
		if project.inherited {
			inherited = append(inherited, pkg)
		}
	}
	if len(inherited) == 0 {
		return nil, nil
	}
	next := inherited[0].Version
	for _, pkg := range inherited[1:] {
		if pkg.Version != next {
			return nil, errors.New(i18n.Tf("java.err.shared_scope_required", pkg.Name))
		}
	}
	rootProject, err := ws.projectFor(root, ".")
	if err != nil {
		return nil, err
	}
	if rootProject.propertyKey != "" {
		if _, err := writeGradleProperty(root, rootProject.propertyKey, next); err != nil {
			return nil, err
		}
		return []string{"gradle.properties"}, nil
	}
	if rootProject.assignment != nil && rootProject.assignment.kind == gradleKindLiteral {
		file := fsx.Join(root, rootProject.assignment.file)
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		quote := string(rootProject.assignment.quote)
		if quote == "" {
			quote = "'"
		}
		text := string(data)
		text = text[:rootProject.assignment.start] + quote + next + quote + text[rootProject.assignment.end:]
		if err := fsx.WriteFileAtomic(file, []byte(text), 0o644); err != nil {
			return nil, err
		}
		return []string{rootProject.assignment.file}, nil
	}
	return nil, errors.New(i18n.Tf("java.err.gradle_version_source", ws.settings.name))
}
