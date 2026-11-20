package java_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/ecosystemtest"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/java"
)

// preflightRound builds a round that releases every detected package of root to
// version. An inherited version has no element of its own, so only the other
// packages appear in Own, exactly like the release executor builds the round.
func preflightRound(t *testing.T, root, version string) ecosystem.VersionRound {
	t.Helper()
	round := ecosystem.VersionRound{}
	for _, pkg := range detect(t, root) {
		pkg.Version = version
		round.Packages = append(round.Packages, pkg)
		if !pkg.Inherited {
			round.Own = append(round.Own, pkg)
		}
	}
	return round
}

func preflight(t *testing.T, root, version string) []ecosystem.PreflightFinding {
	t.Helper()
	capability, ok := java.New().(ecosystem.VersionPreflight)
	if !ok {
		t.Fatal("java adapter does not implement VersionPreflight")
	}
	findings, err := capability.PreflightVersions(root, preflightRound(t, root, version))
	if err != nil {
		t.Fatalf("PreflightVersions: %v", err)
	}
	return findings
}

// extrasPom renders the extras manifest with the given inner XML for the
// internal dependency version.
func extrasPom(version, dependencyVersionInner string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <parent>
    <groupId>com.example</groupId>
    <artifactId>demo-parent</artifactId>
    <version>` + version + `</version>
  </parent>
  <artifactId>extras</artifactId>
  <version>` + version + `</version>
  <dependencies>
    <dependency>
      <groupId>com.example</groupId>
      <artifactId>core</artifactId>
      <version>` + dependencyVersionInner + `</version>
    </dependency>
  </dependencies>
</project>
`
}

// TestMavenPreflightReportsUnsafeElements covers the three inner contents a
// whole-span replacement would drop: a comment, a CDATA section and a child
// element.
func TestMavenPreflightReportsUnsafeElements(t *testing.T) {
	cases := []struct {
		name   string
		inner  string
		reason ecosystem.PreflightReason
	}{
		{"comment", "<!-- keep in sync -->1.2.3-SNAPSHOT", ecosystem.PreflightReasonComment},
		{"cdata", "<![CDATA[1.2.3-SNAPSHOT]]>", ecosystem.PreflightReasonCDATA},
		{"child", "1.2.3-SNAPSHOT<extra>note</extra>", ecosystem.PreflightReasonChild},
		{"processing", "<?keep me?>1.2.3-SNAPSHOT", ecosystem.PreflightReasonProcessing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := ecosystemtest.Fixture(t, "maven-multimodule")
			writeFile(t, root, "extras/pom.xml", extrasPom("1.2.3-SNAPSHOT", tc.inner))
			findings := preflight(t, root, "1.3.0")
			if len(findings) != 1 {
				t.Fatalf("findings = %+v", findings)
			}
			finding := findings[0]
			if finding.Manifest != "extras/pom.xml" || finding.Element != "project/dependencies/dependency/version" {
				t.Fatalf("finding = %+v", finding)
			}
			if finding.Reason != tc.reason {
				t.Fatalf("reason = %v, want %v", finding.Reason, tc.reason)
			}
			if finding.Coordinates != "com.example:core" || finding.Target != "1.3.0" {
				t.Fatalf("finding = %+v", finding)
			}
		})
	}
}

// TestMavenPreflightRejectsUnsafeProjectVersion covers the location of the
// package's own version, which the round writes before any sync runs.
func TestMavenPreflightRejectsUnsafeProjectVersion(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	writeFile(t, root, "extras/pom.xml", strings.Replace(
		extrasPom("1.2.3-SNAPSHOT", "1.2.3-SNAPSHOT"),
		"\n  <version>1.2.3-SNAPSHOT</version>",
		"\n  <version><!-- the release writes this -->1.2.3-SNAPSHOT</version>", 1))
	findings := preflight(t, root, "1.3.0")
	if len(findings) != 1 || findings[0].Element != "project/version" {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Reason != ecosystem.PreflightReasonComment {
		t.Fatalf("reason = %v", findings[0].Reason)
	}
}

// TestMavenPreflightKeepsCompatibleRange keeps [1.0,2.0) verbatim when the
// planned version 1.5.0 still satisfies it, and only updates the surrounding
// version literals.
func TestMavenPreflightKeepsCompatibleRange(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	writeFile(t, root, "extras/pom.xml", extrasPom("1.4.0-SNAPSHOT", "[1.0,2.0)"))
	if findings := preflight(t, root, "1.5.0"); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	syncer, ok := java.New().(ecosystem.VersionSyncer)
	if !ok {
		t.Fatal("java adapter does not implement VersionSyncer")
	}
	pkgs := detect(t, root)
	for i := range pkgs {
		pkgs[i].Version = "1.5.0"
	}
	for _, pkg := range pkgs {
		if pkg.Inherited {
			continue
		}
		if err := java.New().SetVersion(root, pkg, "1.5.0"); err != nil {
			t.Fatalf("SetVersion %s: %v", pkg.Name, err)
		}
	}
	if _, err := syncer.SyncVersions(context.Background(), nil, root, pkgs); err != nil {
		t.Fatalf("SyncVersions: %v", err)
	}
	updated := readFile(t, root, "extras/pom.xml")
	if !strings.Contains(updated, "<version>[1.0,2.0)</version>") {
		t.Fatalf("compatible range was rewritten:\n%s", updated)
	}
	if strings.Count(updated, "<version>1.5.0</version>") != 2 {
		t.Fatalf("only the parent and project versions may change:\n%s", updated)
	}
	if !strings.Contains(readFile(t, root, "core/pom.xml"), "<version>1.5.0</version>") {
		t.Fatal("core parent version was not synced")
	}
}

// TestMavenPreflightStopsOnExcludedRange reports a planned version the range
// does not accept and leaves every manifest untouched.
func TestMavenPreflightStopsOnExcludedRange(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	writeFile(t, root, "extras/pom.xml", extrasPom("1.2.3-SNAPSHOT", "[1.0,2.0)"))
	before := map[string]string{}
	for _, rel := range []string{"pom.xml", "core/pom.xml", "extras/pom.xml"} {
		before[rel] = readFile(t, root, rel)
	}
	findings := preflight(t, root, "2.0.0")
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	finding := findings[0]
	if finding.Reason != ecosystem.PreflightReasonRangeExcluded {
		t.Fatalf("reason = %v", finding.Reason)
	}
	if finding.Range != "[1.0,2.0)" || finding.Target != "2.0.0" || finding.Coordinates != "com.example:core" {
		t.Fatalf("finding = %+v", finding)
	}
	syncer, ok := java.New().(ecosystem.VersionSyncer)
	if !ok {
		t.Fatal("java adapter does not implement VersionSyncer")
	}
	pkgs := detect(t, root)
	for i := range pkgs {
		pkgs[i].Version = "2.0.0"
	}
	_, syncErr := syncer.SyncVersions(context.Background(), nil, root, pkgs)
	if syncErr == nil {
		t.Fatal("SyncVersions must refuse a range the planned version leaves")
	}
	for _, want := range []string{"com.example:core", "[1.0,2.0)", "2.0.0"} {
		if !strings.Contains(syncErr.Error(), want) {
			t.Fatalf("error %q is missing %q", syncErr, want)
		}
	}
	if strings.Contains(syncErr.Error(), "%!") {
		t.Fatalf("error has an unfilled placeholder: %v", syncErr)
	}
	for rel, want := range before {
		if got := readFile(t, root, rel); got != want {
			t.Fatalf("%s changed although the round was refused:\n%s", rel, got)
		}
	}
}

// TestMavenPreflightStopsOnUnreadableRange reports expressions the parser
// cannot evaluate instead of guessing whether the target fits.
func TestMavenPreflightStopsOnUnreadableRange(t *testing.T) {
	for _, inner := range []string{"[1.0,2.0", "(1.0)", "[1.0,2.0,3.0)", "[foo,bar)"} {
		t.Run(inner, func(t *testing.T) {
			root := ecosystemtest.Fixture(t, "maven-multimodule")
			writeFile(t, root, "extras/pom.xml", extrasPom("1.2.3-SNAPSHOT", inner))
			findings := preflight(t, root, "1.3.0")
			if len(findings) != 1 || findings[0].Reason != ecosystem.PreflightReasonRangeUnparsable {
				t.Fatalf("findings = %+v", findings)
			}
			if findings[0].Range != inner {
				t.Fatalf("range = %q", findings[0].Range)
			}
		})
	}
}

// TestMavenSetVersionKeepsBytes rewrites only the version literal: indentation
// around a multi-line value and a comment next to the element survive.
func TestMavenSetVersionKeepsBytes(t *testing.T) {
	root := t.TempDir()
	manifest := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>solo</artifactId>
  <!-- the release rewrites this one -->
  <version>
    1.2.3-SNAPSHOT
  </version>
  <properties>
    <revision>1.2.3-SNAPSHOT</revision>
  </properties>
</project>
`
	writeFile(t, root, "pom.xml", manifest)
	pkgs := detect(t, root)
	if len(pkgs) != 1 {
		t.Fatalf("packages = %+v", pkgs)
	}
	if err := java.New().SetVersion(root, pkgs[0], "1.3.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	want := strings.Replace(manifest,
		"    1.2.3-SNAPSHOT\n  </version>",
		"    1.3.0\n  </version>", 1)
	if got := readFile(t, root, "pom.xml"); got != want {
		t.Fatalf("SetVersion changed more than the version literal:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestMavenSetVersionRefusesUnsafeElement is the direct-write guard: a caller
// that skips the preflight still cannot drop an inner comment.
func TestMavenSetVersionRefusesUnsafeElement(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	before := readFile(t, root, "extras/pom.xml")
	unsafe := strings.Replace(before, "\n  <version>1.2.3-SNAPSHOT</version>",
		"\n  <version><!-- keep -->1.2.3-SNAPSHOT</version>", 1)
	writeFile(t, root, "extras/pom.xml", unsafe)
	byName := byName(detect(t, root))
	setErr := java.New().SetVersion(root, byName["extras"], "1.3.0")
	if setErr == nil {
		t.Fatal("SetVersion must refuse an element with an inner comment")
	}
	if !strings.Contains(setErr.Error(), "project/version") || strings.Contains(setErr.Error(), "%!") {
		t.Fatalf("unexpected refusal message: %v", setErr)
	}
	if got := readFile(t, root, "extras/pom.xml"); got != unsafe {
		t.Fatalf("extras/pom.xml changed although the write was refused:\n%s", got)
	}
}

// TestMavenPreflightReadsParentVersion pins the parent reference as a checked
// location as well.
func TestMavenPreflightReadsParentVersion(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	core := strings.Replace(readFile(t, root, "core/pom.xml"),
		"    <version>1.2.3-SNAPSHOT</version>",
		"    <version><!-- parent pin -->1.2.3-SNAPSHOT</version>", 1)
	writeFile(t, root, "core/pom.xml", core)
	findings := preflight(t, root, "1.3.0")
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Manifest != "core/pom.xml" || findings[0].Element != "project/parent/version" {
		t.Fatalf("finding = %+v", findings[0])
	}
}

// TestMavenPreflightReadsManagedDependency covers dependencyManagement.
func TestMavenPreflightReadsManagedDependency(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	extras := readFile(t, root, "extras/pom.xml")
	managed := strings.Replace(extras, "  </dependencies>",
		"  </dependencies>\n  <dependencyManagement>\n    <dependencies>\n      <dependency>\n"+
			"        <groupId>com.example</groupId>\n        <artifactId>core</artifactId>\n"+
			"        <version><!-- managed pin -->1.2.3-SNAPSHOT</version>\n      </dependency>\n"+
			"    </dependencies>\n  </dependencyManagement>", 1)
	writeFile(t, root, "extras/pom.xml", managed)
	findings := preflight(t, root, "1.3.0")
	found := false
	for _, finding := range findings {
		if finding.Element == "project/dependencyManagement/dependencies/dependency/version" {
			found = true
		}
	}
	if !found {
		t.Fatalf("dependencyManagement pin was not checked: %+v", findings)
	}
}

// TestMavenPreflightNeedsNoWrite checks that the check itself never touches the
// files it inspects.
func TestMavenPreflightNeedsNoWrite(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	writeFile(t, root, "extras/pom.xml", extrasPom("1.2.3-SNAPSHOT", "<!-- keep -->1.2.3-SNAPSHOT"))
	before := map[string]string{}
	for _, rel := range []string{"pom.xml", "core/pom.xml", "extras/pom.xml"} {
		before[rel] = readFile(t, root, rel)
	}
	_ = preflight(t, root, "1.3.0")
	for rel, want := range before {
		if got := readFile(t, root, rel); got != want {
			t.Fatalf("%s changed during the preflight", rel)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "extras", "pom.xml")); err != nil || info.Size() == 0 {
		t.Fatalf("extras manifest: %v", err)
	}
}

// TestMavenPreflightCoversPackageOutsideTheRootReactor covers a configured
// package the root reactor does not list: SetVersion still writes it through
// the reactor of its own directory, so the batch check has to include it.
func TestMavenPreflightCoversPackageOutsideTheRootReactor(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	standalone := `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>standalone</artifactId>
  <version><!-- keep -->1.2.3-SNAPSHOT</version>
</project>
`
	writeFile(t, root, "standalone/pom.xml", standalone)
	capability, ok := java.New().(ecosystem.VersionPreflight)
	if !ok {
		t.Fatal("java adapter does not implement VersionPreflight")
	}
	round := ecosystem.VersionRound{
		Packages: []ecosystem.Package{
			{Name: "demo-parent", Path: ".", Resolver: "java", Version: "1.3.0", Manifest: "pom.xml"},
			{Name: "standalone", Path: "standalone", Resolver: "java", Version: "1.3.0", Manifest: "standalone/pom.xml"},
		},
		Own: []ecosystem.Package{
			{Name: "demo-parent", Path: ".", Resolver: "java", Version: "1.3.0", Manifest: "pom.xml"},
			{Name: "standalone", Path: "standalone", Resolver: "java", Version: "1.3.0", Manifest: "standalone/pom.xml"},
		},
	}
	findings, err := capability.PreflightVersions(root, round)
	if err != nil {
		t.Fatalf("PreflightVersions: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Manifest != "standalone/pom.xml" || findings[0].Element != "project/version" {
		t.Fatalf("finding = %+v", findings[0])
	}
}

// soloPom renders a standalone Maven project whose project version holds the
// given inner content.
func soloPom(artifact, versionInner string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>` + artifact + `</artifactId>
  <version>` + versionInner + `</version>
</project>
`
}

// TestMavenPreflightReadsRevisionProperty covers the CI-friendly version
// source: the release rewrites <properties><revision>, so it is checked too.
func TestMavenPreflightReadsRevisionProperty(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-revision")
	if findings := preflight(t, root, "3.4.0"); len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	writeFile(t, root, "pom.xml", strings.Replace(readFile(t, root, "pom.xml"),
		"    <revision>3.3.0</revision>",
		"    <revision><!-- keep -->3.3.0</revision>", 1))
	findings := preflight(t, root, "3.4.0")
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	finding := findings[0]
	if finding.Manifest != "pom.xml" || finding.Element != "project/properties/revision" {
		t.Fatalf("finding = %+v", finding)
	}
	if finding.Reason != ecosystem.PreflightReasonComment || finding.Target != "3.4.0" {
		t.Fatalf("finding = %+v", finding)
	}
}

// TestMavenPreflightWithoutRootPom covers a repository where every package is
// loaded with the reactor of its own directory.
func TestMavenPreflightWithoutRootPom(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "alpha/pom.xml", soloPom("alpha", "1.2.3-SNAPSHOT"))
	writeFile(t, root, "beta/pom.xml", soloPom("beta", "<!-- keep -->1.2.3-SNAPSHOT"))
	capability, ok := java.New().(ecosystem.VersionPreflight)
	if !ok {
		t.Fatal("java adapter does not implement VersionPreflight")
	}
	round := ecosystem.VersionRound{
		Packages: []ecosystem.Package{
			{Name: "alpha", Path: "alpha", Resolver: "java", Version: "1.3.0"},
			{Name: "beta", Path: "beta", Resolver: "java", Version: "1.3.0"},
		},
		Own: []ecosystem.Package{
			{Name: "alpha", Path: "alpha", Resolver: "java", Version: "1.3.0"},
			{Name: "beta", Path: "beta", Resolver: "java", Version: "1.3.0"},
		},
	}
	findings, err := capability.PreflightVersions(root, round)
	if err != nil {
		t.Fatalf("PreflightVersions: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Manifest != "beta/pom.xml" || findings[0].Element != "project/version" {
		t.Fatalf("finding = %+v", findings[0])
	}
	// Repairing the element clears the round.
	writeFile(t, root, "beta/pom.xml", soloPom("beta", "1.2.3-SNAPSHOT"))
	findings, err = capability.PreflightVersions(root, round)
	if err != nil {
		t.Fatalf("PreflightVersions: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

// TestMavenPreflightIgnoresExternalRanges keeps the range rule on internal
// coordinates: a range qegg never rewrites must not stop a round.
func TestMavenPreflightIgnoresExternalRanges(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	extras := readFile(t, root, "extras/pom.xml")
	external := "    <dependency>\n      <groupId>org.example</groupId>\n      <artifactId>external</artifactId>\n      <version>[1.0,2.0)</version>\n    </dependency>\n  </dependencies>"
	writeFile(t, root, "extras/pom.xml", strings.Replace(extras, "  </dependencies>", external, 1))
	if findings := preflight(t, root, "2.0.0"); len(findings) != 0 {
		t.Fatalf("an external range must not stop the round: %+v", findings)
	}
	syncer, ok := java.New().(ecosystem.VersionSyncer)
	if !ok {
		t.Fatal("java adapter does not implement VersionSyncer")
	}
	pkgs := detect(t, root)
	for i := range pkgs {
		pkgs[i].Version = "2.0.0"
	}
	if _, err := syncer.SyncVersions(context.Background(), nil, root, pkgs); err != nil {
		t.Fatalf("SyncVersions: %v", err)
	}
	updated := readFile(t, root, "extras/pom.xml")
	if !strings.Contains(updated, "<version>[1.0,2.0)</version>") {
		t.Fatalf("an external range was rewritten:\n%s", updated)
	}
	if !strings.Contains(updated, "<artifactId>core</artifactId>") || !strings.Contains(updated, "<version>2.0.0</version>") {
		t.Fatalf("the internal pin was not synced:\n%s", updated)
	}
}

// TestMavenPreflightReportsEmptyPin reports a pin without a version instead of
// leaving it behind silently.
func TestMavenPreflightReportsEmptyPin(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	writeFile(t, root, "extras/pom.xml", extrasPom("1.2.3-SNAPSHOT", ""))
	findings := preflight(t, root, "1.3.0")
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	finding := findings[0]
	if finding.Reason != ecosystem.PreflightReasonEmpty || finding.Coordinates != "com.example:core" {
		t.Fatalf("finding = %+v", finding)
	}
	if finding.Element != "project/dependencies/dependency/version" {
		t.Fatalf("finding = %+v", finding)
	}
}
