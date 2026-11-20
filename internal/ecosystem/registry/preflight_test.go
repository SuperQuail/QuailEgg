package registry_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/ecosystemtest"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/registry"
)

// mavenRound releases every package of the maven-multimodule fixture to version.
func mavenRound(version string) ecosystem.VersionRound {
	return ecosystem.VersionRound{
		Packages: []ecosystem.Package{
			{Name: "demo-parent", Path: ".", Resolver: "java", Version: version},
			{Name: "core", Path: "core", Resolver: "java", Version: version, Inherited: true},
			{Name: "extras", Path: "extras", Resolver: "java", Version: version},
		},
		Own: []ecosystem.Package{
			{Name: "demo-parent", Path: ".", Resolver: "java", Version: version},
			{Name: "extras", Path: "extras", Resolver: "java", Version: version},
		},
	}
}

// rewriteExtras replaces the version literal of the fixture's internal core
// dependency. The search starts after the core artifactId so the parent and
// project versions keep their own value.
func rewriteExtras(t *testing.T, root, value string) {
	t.Helper()
	path := filepath.Join(root, "extras", "pom.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	anchor := strings.Index(text, "<artifactId>core</artifactId>")
	if anchor < 0 {
		t.Fatal("extras/pom.xml has no core dependency")
	}
	const literal = "<version>1.2.3-SNAPSHOT</version>"
	offset := strings.Index(text[anchor:], literal)
	if offset < 0 {
		t.Fatal("extras/pom.xml has no core version pin")
	}
	start := anchor + offset
	updated := text[:start] + "<version>" + value + "</version>" + text[start+len(literal):]
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPreflightVersionsReportsUnsafeManifest pushes the finding through the
// registry and checks the rendered message: the file, the element and the fix.
func TestPreflightVersionsReportsUnsafeManifest(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	rewriteExtras(t, root, "<!-- keep in sync -->1.2.3-SNAPSHOT")
	findings, err := registry.PreflightVersions(root, mavenRound("1.3.0"))
	if err != nil {
		t.Fatalf("PreflightVersions: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Manifest != "extras/pom.xml" || findings[0].Element != "project/dependencies/dependency/version" {
		t.Fatalf("finding = %+v", findings[0])
	}
	message := registry.PreflightMessage(findings)
	for _, want := range []string{"nothing was written", "extras/pom.xml", "project/dependencies/dependency/version", "comment"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q is missing %q", message, want)
		}
	}
}

// TestPreflightVersionsReportsExcludedRange renders coordinates, range and
// target without dumping the manifest.
func TestPreflightVersionsReportsExcludedRange(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	rewriteExtras(t, root, "[1.0,2.0)")
	findings, err := registry.PreflightVersions(root, mavenRound("2.0.0"))
	if err != nil {
		t.Fatalf("PreflightVersions: %v", err)
	}
	if len(findings) != 1 || findings[0].Reason != ecosystem.PreflightReasonRangeExcluded {
		t.Fatalf("findings = %+v", findings)
	}
	message := registry.PreflightMessage(findings)
	for _, want := range []string{"com.example:core", "[1.0,2.0)", "2.0.0", "release plan"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q is missing %q", message, want)
		}
	}
}

// TestPreflightVersionsKeepsCompatibleRange leaves a satisfied expression alone.
func TestPreflightVersionsKeepsCompatibleRange(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	rewriteExtras(t, root, "[1.0,2.0)")
	findings, err := registry.PreflightVersions(root, mavenRound("1.5.0"))
	if err != nil {
		t.Fatalf("PreflightVersions: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

// TestPreflightVersionsSkipsEcosystemsWithoutTheCapability keeps the check
// free for ecosystems that do not implement it.
func TestPreflightVersionsSkipsEcosystemsWithoutTheCapability(t *testing.T) {
	root := ecosystemtest.Fixture(t, "npm-basic")
	round := ecosystem.VersionRound{
		Packages: []ecosystem.Package{{Name: "demo", Path: ".", Resolver: "npm", Version: "2.0.0"}},
		Own:      []ecosystem.Package{{Name: "demo", Path: ".", Resolver: "npm", Version: "2.0.0"}},
	}
	findings, err := registry.PreflightVersions(root, round)
	if err != nil {
		t.Fatalf("PreflightVersions: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
}

// TestPreflightMessageRendersEveryReason keeps every reason and every catalog
// key wired together: a renamed key would otherwise show up as the key itself,
// and two pins of one file must stay distinguishable by their coordinate.
func TestPreflightMessageRendersEveryReason(t *testing.T) {
	cases := []struct {
		name    string
		finding ecosystem.PreflightFinding
		want    string
	}{
		{"comment", ecosystem.PreflightFinding{Reason: ecosystem.PreflightReasonComment}, "comment"},
		{"cdata", ecosystem.PreflightFinding{Reason: ecosystem.PreflightReasonCDATA}, "CDATA"},
		{"child", ecosystem.PreflightFinding{Reason: ecosystem.PreflightReasonChild}, "child element"},
		{"processing", ecosystem.PreflightFinding{Reason: ecosystem.PreflightReasonProcessing}, "processing instruction"},
		{"empty", ecosystem.PreflightFinding{Reason: ecosystem.PreflightReasonEmpty}, "is empty"},
		{
			"range_excluded",
			ecosystem.PreflightFinding{Reason: ecosystem.PreflightReasonRangeExcluded, Range: "[1.0,2.0)", Target: "2.0.0"},
			"does not accept the planned version 2.0.0",
		},
		{
			"range_unparsable",
			ecosystem.PreflightFinding{Reason: ecosystem.PreflightReasonRangeUnparsable, Range: "[1.0", Target: "1.3.0"},
			"cannot interpret",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			finding := tc.finding
			finding.Manifest = "extras/pom.xml"
			finding.Element = "project/dependencies/dependency/version"
			finding.Coordinates = "com.example:core"
			message := registry.PreflightMessage([]ecosystem.PreflightFinding{finding})
			for _, want := range []string{"nothing was written", "extras/pom.xml", "project/dependencies/dependency/version", "com.example:core", tc.want} {
				if !strings.Contains(message, want) {
					t.Fatalf("message %q is missing %q", message, want)
				}
			}
		})
	}
}
