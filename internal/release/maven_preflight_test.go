package release

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/git"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

// mavenReactorPoms renders the three manifests of a Maven reactor: a parent
// pom, a core module that inherits its version, and an extras module that
// carries its own version and depends on core from both dependency lists. The
// version elements are laid out with indentation and sibling comments so a
// byte-level comparison shows whether anything but the version literal moved.
func mavenReactorPoms(version, corePin string) map[string]string {
	render := func(text string) string {
		text = strings.ReplaceAll(text, "@V@", version)
		return strings.ReplaceAll(text, "@PIN@", corePin)
	}
	return map[string]string{
		"pom.xml": render(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo-parent</artifactId>
  <!-- this comment sits next to the version -->
  <version>@V@</version>
  <packaging>pom</packaging>
  <modules>
    <module>core</module>
    <module>extras</module>
  </modules>
  <distributionManagement>
    <repository>
      <id>local</id>
      <url>file:///tmp/qegg-repo</url>
    </repository>
  </distributionManagement>
</project>
`),
		"core/pom.xml": render(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>com.example</groupId>
    <artifactId>demo-parent</artifactId>
    <version>@V@</version>
  </parent>
  <artifactId>core</artifactId>
</project>
`),
		"extras/pom.xml": render(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>com.example</groupId>
    <artifactId>demo-parent</artifactId>
    <version>@V@</version>
  </parent>
  <artifactId>extras</artifactId>
  <version>
    @V@
  </version>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>core</artifactId>
      <!-- core is pinned here on purpose -->
      <version>@PIN@</version>
    </dependency>
  </dependencies>
  <dependencyManagement>
    <dependencies>
      <dependency>
        <groupId>com.example</groupId>
        <artifactId>core</artifactId>
        <version>@PIN@</version>
      </dependency>
    </dependencies>
  </dependencyManagement>
</project>
`),
	}
}

// unsafePin places a comment inside the first internal dependency version
// element of extras, the location a whole-span rewrite would silently drop.
func unsafePin(poms map[string]string) map[string]string {
	poms["extras/pom.xml"] = strings.Replace(poms["extras/pom.xml"],
		"      <version>1.2.3-SNAPSHOT</version>",
		"      <version><!-- keep in sync -->1.2.3-SNAPSHOT</version>", 1)
	return poms
}

// newMavenReactorRepo writes the manifests and one change record, then commits
// everything so the release preflight sees a clean tree.
func newMavenReactorRepo(t *testing.T, poms map[string]string, bump string) string {
	t.Helper()
	execx.SanitizeGitHookEnv()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "release@example.com")
	gitRun(t, dir, "config", "user.name", "release")
	for rel, body := range poms {
		writeMavenFile(t, dir, rel, body)
	}
	writeMavenFile(t, dir, ".changes/README.md", "# Change records\n")
	record := "---\ntype: feat\npackages:\n  demo-parent: " + bump + "\n  core: " + bump + "\n  extras: " + bump + "\n---\n\nAdd a feature.\n"
	writeMavenFile(t, dir, ".changes/add-feature.md", record)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "feat: add feature")
	return dir
}

func writeMavenFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMavenFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// mavenReleaseDeps registers the three Maven packages as one shared version.
func mavenReleaseDeps(t *testing.T, dir string) *Deps {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("GITHUB_ACTIONS", "")
	repo, err := git.Open(context.Background(), dir, execx.OSRunner{})
	if err != nil {
		t.Fatalf("git.Open: %v", err)
	}
	cfg := config.Default()
	publish := false
	cfg.Packages = map[string]config.Package{
		"demo-parent": {Path: ".", Resolver: "java", Publish: &publish},
		"core":        {Path: "core", Resolver: "java", Publish: &publish},
		"extras":      {Path: "extras", Resolver: "java", Publish: &publish},
	}
	cfg.Release.VersionScope = config.VersionScopeShared
	cfg.Release.Mode = config.ModeCommit
	cfg.Release.GitHubRelease = false
	cfg.Release.CommitMessage = "chore(release): bump versions"
	cfg.Verify.Commands = nil
	return &Deps{
		Root: dir,
		Cfg:  cfg,
		Git:  repo,
		Run:  execx.OSRunner{},
		UI:   ui.Discard(),
		Now:  time.Now,
		Options: Options{
			SkipVerify: true, NoPublish: true, NoPush: true, NoGitHubRelease: true,
		},
	}
}

// assertManifestsUnchanged compares every manifest byte for byte and checks
// that git tracked nothing new.
func assertManifestsUnchanged(t *testing.T, dir string, poms map[string]string) {
	t.Helper()
	for rel, want := range poms {
		if got := readMavenFile(t, dir, rel); got != want {
			t.Fatalf("%s changed although the round stopped:\n%s", rel, got)
		}
	}
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--untracked-files=no").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("tracked files changed although the round stopped:\n%s", out)
	}
}

// TestMavenPreflightStopsBeforeAnyWrite puts the problem in the last package of
// the plan and asserts that no manifest is rewritten at all.
func TestMavenPreflightStopsBeforeAnyWrite(t *testing.T) {
	poms := unsafePin(mavenReactorPoms("1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT"))
	dir := newMavenReactorRepo(t, poms, "minor")
	d := mavenReleaseDeps(t, dir)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Phase != PhaseVersion || plan.Version.Next != "1.3.0" {
		t.Fatalf("plan = %+v", plan.Version)
	}
	if last := plan.Packages[len(plan.Packages)-1]; last.Name != "extras" {
		t.Fatalf("the unsafe package must be written last, plan = %+v", plan.Packages)
	}
	err = Run(ctx, d, plan)
	if err == nil {
		t.Fatal("the release must stop before it rewrites a manifest")
	}
	for _, want := range []string{"extras/pom.xml", "project/dependencies/dependency/version", "comment"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q is missing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "%!") {
		t.Fatalf("error has an unfilled placeholder: %v", err)
	}
	assertManifestsUnchanged(t, dir, poms)
	if _, statErr := os.Stat(filepath.Join(dir, "CHANGELOG.md")); !os.IsNotExist(statErr) {
		t.Fatal("the failed round wrote a changelog")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".changes", "add-feature.md")); statErr != nil {
		t.Fatal("the failed round consumed the change record")
	}
	if hasTag(t, dir, "v1.3.0") {
		t.Fatal("the failed round created a tag")
	}
}

// TestMavenPreflightRewritesOnlyVersionLiterals fixes the input and reruns the
// normal entry point: the comments and the indentation survive, and only the
// version literals change.
func TestMavenPreflightRewritesOnlyVersionLiterals(t *testing.T) {
	broken := unsafePin(mavenReactorPoms("1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT"))
	dir := newMavenReactorRepo(t, broken, "minor")
	d := mavenReleaseDeps(t, dir)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Run(ctx, d, plan); err == nil {
		t.Fatal("the first round must stop on the inner comment")
	}
	// The maintainer moves the comment out of the element and commits the fix,
	// then reruns the normal entry point.
	writeMavenFile(t, dir, "extras/pom.xml", mavenReactorPoms("1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT")["extras/pom.xml"])
	gitRun(t, dir, "commit", "-qam", "fix: move the pin comment")
	second, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build after the fix: %v", err)
	}
	if err := Run(ctx, d, second); err != nil {
		t.Fatalf("Run after the fix: %v", err)
	}
	expected := mavenReactorPoms("1.3.0", "1.3.0")
	for rel, want := range expected {
		if got := readMavenFile(t, dir, rel); got != want {
			t.Fatalf("%s does not match the expected bytes:\ngot:\n%s\nwant:\n%s", rel, got, want)
		}
	}
	if !hasTag(t, dir, "v1.3.0") {
		t.Fatal("the fixed round did not tag the release")
	}
}

// TestMavenDependencyRangeInside covers [1.0,2.0) with a planned version it
// still accepts: the expression stays verbatim.
func TestMavenDependencyRangeInside(t *testing.T) {
	poms := mavenReactorPoms("1.4.0-SNAPSHOT", "[1.0,2.0)")
	dir := newMavenReactorRepo(t, poms, "minor")
	d := mavenReleaseDeps(t, dir)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Version.Next != "1.5.0" {
		t.Fatalf("next = %q, want 1.5.0", plan.Version.Next)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	expected := mavenReactorPoms("1.5.0", "[1.0,2.0)")
	for rel, want := range expected {
		if got := readMavenFile(t, dir, rel); got != want {
			t.Fatalf("%s does not match the expected bytes:\ngot:\n%s\nwant:\n%s", rel, got, want)
		}
	}
	if got := readMavenFile(t, dir, "extras/pom.xml"); strings.Count(got, "[1.0,2.0)") != 2 {
		t.Fatalf("a compatible range was rewritten:\n%s", got)
	}
}

// TestMavenDependencyRangeOutside covers [1.0,2.0) with a planned version it
// excludes: the release stops before writing and names the coordinates, the
// range and the target.
func TestMavenDependencyRangeOutside(t *testing.T) {
	poms := mavenReactorPoms("1.2.3-SNAPSHOT", "[1.0,2.0)")
	dir := newMavenReactorRepo(t, poms, "major")
	d := mavenReleaseDeps(t, dir)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Version.Next != "2.0.0" {
		t.Fatalf("next = %q, want 2.0.0", plan.Version.Next)
	}
	err = Run(ctx, d, plan)
	if err == nil {
		t.Fatal("the release must stop on a range the planned version leaves")
	}
	for _, want := range []string{"extras/pom.xml", "com.example:core", "[1.0,2.0)", "2.0.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q is missing %q", err, want)
		}
	}
	assertManifestsUnchanged(t, dir, poms)
	if hasTag(t, dir, "v2.0.0") {
		t.Fatal("the stopped round created a tag")
	}
}

// TestMavenPreflightLeavesVersionFileUntouched proves the version-file write
// stays behind the batch check. The reactor is the three-package fixture with
// the problem in the last package, plus a configured version file: the release
// stops, every manifest and the version file are byte-identical, and the fixed
// rerun then updates the version file together with the manifests.
func TestMavenPreflightLeavesVersionFileUntouched(t *testing.T) {
	versionFile := "{\n  \"productName\": \"demo\",\n  \"version\": \"1.2.3-SNAPSHOT\",\n  \"bundle\": {\n    \"active\": true\n  }\n}\n"
	poms := unsafePin(mavenReactorPoms("1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT"))
	dir := newMavenReactorRepo(t, poms, "minor")
	writeMavenFile(t, dir, "version.json", versionFile)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "chore: track the version file")
	d := mavenReleaseDeps(t, dir)
	d.Cfg.Version.File = []config.VersionFile{{Path: "version.json", Format: "json", Key: "version"}}
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Version.Next != "1.3.0" {
		t.Fatalf("next = %q, want 1.3.0", plan.Version.Next)
	}
	if last := plan.Packages[len(plan.Packages)-1]; last.Name != "extras" {
		t.Fatalf("the unsafe package must be written last, plan = %+v", plan.Packages)
	}
	if err := Run(ctx, d, plan); err == nil {
		t.Fatal("the release must stop before it rewrites a manifest")
	}
	untouched := map[string]string{"version.json": versionFile}
	for rel, body := range poms {
		untouched[rel] = body
	}
	assertManifestsUnchanged(t, dir, untouched)

	// The maintainer fixes the manifest, and the next round updates both the
	// manifests and the version file.
	writeMavenFile(t, dir, "extras/pom.xml", mavenReactorPoms("1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT")["extras/pom.xml"])
	gitRun(t, dir, "commit", "-qam", "fix: move the pin comment")
	second, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build after the fix: %v", err)
	}
	if err := Run(ctx, d, second); err != nil {
		t.Fatalf("Run after the fix: %v", err)
	}
	want := strings.Replace(versionFile, "1.2.3-SNAPSHOT", "1.3.0", 1)
	if got := readMavenFile(t, dir, "version.json"); got != want {
		t.Fatalf("version file does not match the expected bytes:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// mavenLegacyPom renders a module that is part of the reactor but not a
// configured package: it inherits the parent version and pins the internal
// extras artifact, so the reference sync rewrites both of them.
func mavenLegacyPom(version, extrasPin string) string {
	render := func(text string) string {
		text = strings.ReplaceAll(text, "@V@", version)
		return strings.ReplaceAll(text, "@PIN@", extrasPin)
	}
	return render(`<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>com.example</groupId>
    <artifactId>demo-parent</artifactId>
    <version>@V@</version>
  </parent>
  <artifactId>legacy</artifactId>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>extras</artifactId>
      <version>@PIN@</version>
    </dependency>
  </dependencies>
</project>
`)
}

// withLegacyModule adds the unplanned module to the root pom of the fixture.
func withLegacyModule(poms map[string]string) map[string]string {
	poms["pom.xml"] = strings.Replace(poms["pom.xml"],
		"    <module>extras</module>\n",
		"    <module>extras</module>\n    <module>legacy</module>\n", 1)
	return poms
}

// crlfMap renders every manifest with CRLF line endings.
func crlfMap(poms map[string]string) map[string]string {
	out := make(map[string]string, len(poms))
	for rel, body := range poms {
		out[rel] = strings.ReplaceAll(body, "\n", "\r\n")
	}
	return out
}

// TestMavenPreflightCoversUnplannedModule guards the inspected set: the round
// rewrites the parent reference and the internal pins of every reactor module,
// so a module qegg does not release still has to pass the check. A version phase
// that only inspected planned packages would rewrite the manifests first and
// notice the unplanned module later.
func TestMavenPreflightCoversUnplannedModule(t *testing.T) {
	poms := withLegacyModule(mavenReactorPoms("1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT"))
	poms["legacy/pom.xml"] = strings.Replace(mavenLegacyPom("1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT"),
		"      <version>1.2.3-SNAPSHOT</version>",
		"      <version><!-- pinned by hand -->1.2.3-SNAPSHOT</version>", 1)
	dir := newMavenReactorRepo(t, poms, "minor")
	d := mavenReleaseDeps(t, dir)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, pkg := range plan.Packages {
		if pkg.Name == "legacy" {
			t.Fatalf("legacy must stay unplanned, plan = %+v", plan.Packages)
		}
	}
	err = Run(ctx, d, plan)
	if err == nil {
		t.Fatal("the round must stop on the unplanned module before it writes")
	}
	for _, want := range []string{"legacy/pom.xml", "com.example:extras", "comment"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q is missing %q", err, want)
		}
	}
	assertManifestsUnchanged(t, dir, poms)

	// Once the module is repaired the round rewrites it together with the rest.
	writeMavenFile(t, dir, "legacy/pom.xml", mavenLegacyPom("1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT"))
	gitRun(t, dir, "commit", "-qam", "fix: move the pin comment")
	second, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build after the fix: %v", err)
	}
	if err := Run(ctx, d, second); err != nil {
		t.Fatalf("Run after the fix: %v", err)
	}
	expected := withLegacyModule(mavenReactorPoms("1.3.0", "1.3.0"))
	expected["legacy/pom.xml"] = mavenLegacyPom("1.3.0", "1.3.0")
	for rel, want := range expected {
		if got := readMavenFile(t, dir, rel); got != want {
			t.Fatalf("%s does not match the expected bytes:\ngot:\n%s\nwant:\n%s", rel, got, want)
		}
	}
}

// TestMavenPreflightKeepsCRLF checks the splice on CRLF manifests: only the
// version literals change, the line endings stay as they were.
func TestMavenPreflightKeepsCRLF(t *testing.T) {
	poms := crlfMap(mavenReactorPoms("1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT"))
	dir := newMavenReactorRepo(t, poms, "minor")
	if got := readMavenFile(t, dir, "extras/pom.xml"); !strings.Contains(got, "\r\n") {
		t.Fatal("the fixture must use CRLF line endings")
	}
	d := mavenReleaseDeps(t, dir)
	ctx := context.Background()
	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	expected := crlfMap(mavenReactorPoms("1.3.0", "1.3.0"))
	for rel, want := range expected {
		if got := readMavenFile(t, dir, rel); got != want {
			t.Fatalf("%s changed its line endings:\ngot:  %q\nwant: %q", rel, got, want)
		}
	}
}
