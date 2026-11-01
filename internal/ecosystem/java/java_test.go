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

func adapter() ecosystem.Ecosystem { return java.New() }

func byName(pkgs []ecosystem.Package) map[string]ecosystem.Package {
	out := make(map[string]ecosystem.Package, len(pkgs))
	for _, pkg := range pkgs {
		out[pkg.Name] = pkg
	}
	return out
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func detect(t *testing.T, root string) []ecosystem.Package {
	t.Helper()
	pkgs, err := adapter().Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return pkgs
}

func TestSpec(t *testing.T) {
	spec := java.New().Spec()
	if spec.ID != "java" || spec.Language != "Java" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	if java.New().Publisher().Name() != "java" {
		t.Fatal("unexpected publisher name")
	}
}

func TestDetectMavenMultimodule(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	pkgs := detect(t, root)
	if len(pkgs) != 3 {
		t.Fatalf("packages = %+v", pkgs)
	}
	byName := byName(pkgs)
	parent := byName["demo-parent"]
	if parent.Version != "1.2.3-SNAPSHOT" || parent.Inherited || !parent.Publish {
		t.Fatalf("parent = %+v", parent)
	}
	core := byName["core"]
	if core.Version != "1.2.3-SNAPSHOT" || !core.Inherited || !core.Publish {
		t.Fatalf("core = %+v", core)
	}
	extras := byName["extras"]
	if extras.Version != "1.2.3-SNAPSHOT" || extras.Inherited {
		t.Fatalf("extras = %+v", extras)
	}
	if !contains(extras.Deps, "core") {
		t.Fatalf("extras deps = %v", extras.Deps)
	}
}

func TestMavenSetVersionAndSync(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-multimodule")
	adapter := java.New()
	pkgs := detect(t, root)
	byName := byName(pkgs)
	if err := adapter.SetVersion(root, byName["demo-parent"], "1.3.0"); err != nil {
		t.Fatalf("SetVersion root: %v", err)
	}
	if err := adapter.SetVersion(root, byName["extras"], "1.3.0"); err != nil {
		t.Fatalf("SetVersion extras: %v", err)
	}
	repo, ok := adapter.(ecosystem.VersionSyncer)
	if !ok {
		t.Fatal("java adapter does not implement VersionSyncer")
	}
	updated := []ecosystem.Package{
		{Name: "demo-parent", Path: ".", Version: "1.3.0", Manifest: "pom.xml"},
		{Name: "core", Path: "core", Version: "1.3.0", Inherited: true, Manifest: "core/pom.xml"},
		{Name: "extras", Path: "extras", Version: "1.3.0", Manifest: "extras/pom.xml"},
	}
	changed, err := repo.SyncVersions(context.Background(), nil, root, updated)
	if err != nil {
		t.Fatalf("SyncVersions: %v", err)
	}
	if !contains(changed, "core/pom.xml") || !contains(changed, "extras/pom.xml") {
		t.Fatalf("changed = %v", changed)
	}
	if !strings.Contains(readFile(t, root, "pom.xml"), "<version>1.3.0</version>") {
		t.Fatal("root version not written")
	}
	core := readFile(t, root, "core/pom.xml")
	if !strings.Contains(core, "<version>1.3.0</version>") {
		t.Fatalf("core parent not synced:\n%s", core)
	}
	extras := readFile(t, root, "extras/pom.xml")
	if !strings.Contains(extras, "<version>1.3.0</version>") {
		t.Fatalf("extras parent not synced:\n%s", extras)
	}
	if strings.Count(extras, "1.3.0") < 3 {
		t.Fatalf("extras dependency pin not synced:\n%s", extras)
	}
}

func TestMavenRevisionVersion(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-revision")
	pkgs := detect(t, root)
	byName := byName(pkgs)
	if byName["revision-parent"].Version != "3.3.0" || byName["api"].Version != "3.3.0" || !byName["api"].Inherited {
		t.Fatalf("packages = %+v", pkgs)
	}
	adapter := java.New()
	if err := adapter.SetVersion(root, byName["revision-parent"], "3.4.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	if !strings.Contains(readFile(t, root, "pom.xml"), "<revision>3.4.0</revision>") {
		t.Fatal("revision property not written")
	}
	if !strings.Contains(readFile(t, root, "api/pom.xml"), "${revision}") {
		t.Fatal("child placeholder should stay untouched")
	}
}

func TestMavenExternalParentIsNotAPackage(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-external-parent")
	if pkgs := detect(t, root); len(pkgs) != 0 {
		t.Fatalf("packages = %+v", pkgs)
	}
	scanner, ok := java.New().(ecosystem.ManifestScanner)
	if !ok {
		t.Fatal("java adapter does not implement ManifestScanner")
	}
	scan, err := scanner.ScanManifest(root, ecosystem.Package{Path: ".", Manifest: "pom.xml", Resolver: "java"})
	if err != nil {
		t.Fatalf("ScanManifest: %v", err)
	}
	if scan.OK || scan.Reason != ecosystem.ScanReasonUnsupportedVersion || scan.Detail == "" {
		t.Fatalf("scan = %+v", scan)
	}
}

func TestGradleMultiproject(t *testing.T) {
	root := ecosystemtest.Fixture(t, "gradle-multiproject")
	pkgs := detect(t, root)
	if len(pkgs) != 3 {
		t.Fatalf("packages = %+v", pkgs)
	}
	byName := byName(pkgs)
	rootPkg := byName["gradle-demo"]
	if rootPkg.Version != "2.8.0-SNAPSHOT" || rootPkg.Inherited || rootPkg.Publish {
		t.Fatalf("root = %+v", rootPkg)
	}
	for _, name := range []string{"app", "lib"} {
		pkg := byName[name]
		if pkg.Version != "2.8.0-SNAPSHOT" || !pkg.Inherited || !pkg.Publish {
			t.Fatalf("%s = %+v", name, pkg)
		}
	}
}

func TestGradlePropertyVersionUpdate(t *testing.T) {
	root := ecosystemtest.Fixture(t, "gradle-multiproject")
	adapter := java.New()
	pkgs := byName(detect(t, root))
	rootPkg := pkgs["gradle-demo"]
	if err := adapter.SetVersion(root, rootPkg, "2.9.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	if !strings.Contains(readFile(t, root, "gradle.properties"), "mod_version=2.9.0") {
		t.Fatal("gradle.properties not updated")
	}
	reporter, ok := adapter.(ecosystem.VersionPathReporter)
	if !ok {
		t.Fatal("java adapter does not implement VersionPathReporter")
	}
	paths, err := reporter.VersionPaths(root, rootPkg, "2.9.0")
	if err != nil {
		t.Fatalf("VersionPaths: %v", err)
	}
	if len(paths) != 1 || paths[0] != "gradle.properties" {
		t.Fatalf("paths = %v", paths)
	}
	syncer, ok := adapter.(ecosystem.VersionSyncer)
	if !ok {
		t.Fatal("java adapter does not implement VersionSyncer")
	}
	updated := []ecosystem.Package{
		{Name: "gradle-demo", Path: ".", Version: "2.9.0", Manifest: "build.gradle"},
		{Name: "app", Path: "app", Version: "2.9.0", Inherited: true, Manifest: "app/build.gradle"},
		{Name: "lib", Path: "lib", Version: "2.9.0", Inherited: true, Manifest: "lib/build.gradle"},
	}
	changed, err := syncer.SyncVersions(context.Background(), nil, root, updated)
	if err != nil {
		t.Fatalf("SyncVersions: %v", err)
	}
	if !contains(changed, "gradle.properties") {
		t.Fatalf("changed = %v", changed)
	}
	if !strings.Contains(readFile(t, root, "gradle.properties"), "mod_version=2.9.0") {
		t.Fatal("gradle.properties not synced")
	}
	if byName(detect(t, root))["gradle-demo"].Version != "2.9.0" {
		t.Fatal("detected version not updated")
	}
}

func TestGradleLiteralVersion(t *testing.T) {
	root := ecosystemtest.Fixture(t, "gradle-literal")
	pkgs := detect(t, root)
	if len(pkgs) != 1 || pkgs[0].Version != "1.2.3" || !pkgs[0].Publish {
		t.Fatalf("packages = %+v", pkgs)
	}
	if err := java.New().SetVersion(root, pkgs[0], "1.3.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	if !strings.Contains(readFile(t, root, "build.gradle"), "version = '1.3.0'") {
		t.Fatal("literal version not written")
	}
}

func TestGradleProjectPropertyVersion(t *testing.T) {
	root := ecosystemtest.Fixture(t, "gradle-property")
	pkgs := detect(t, root)
	if len(pkgs) != 1 || pkgs[0].Version != "3.19.0-SNAPSHOT" {
		t.Fatalf("packages = %+v", pkgs)
	}
	if err := java.New().SetVersion(root, pkgs[0], "3.20.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	if !strings.Contains(readFile(t, root, "gradle.properties"), "VERSION_NAME=3.20.0") {
		t.Fatal("property version not written")
	}
}

func TestGradleVersionCodeIsNotAProjectVersion(t *testing.T) {
	root := ecosystemtest.Fixture(t, "gradle-versioncode")
	if pkgs := detect(t, root); len(pkgs) != 0 {
		t.Fatalf("packages = %+v", pkgs)
	}
	scanner, ok := java.New().(ecosystem.ManifestScanner)
	if !ok {
		t.Fatal("java adapter does not implement ManifestScanner")
	}
	scan, err := scanner.ScanManifest(root, ecosystem.Package{Path: "app", Manifest: "app/build.gradle.kts", Resolver: "java"})
	if err != nil {
		t.Fatalf("ScanManifest: %v", err)
	}
	if scan.OK || scan.Reason != ecosystem.ScanReasonUnsupportedVersion {
		t.Fatalf("scan = %+v", scan)
	}
}

func TestGradlePublishDetection(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   bool
	}{
		{"comment-only", "plugins { id 'java' }\n// id 'maven-publish'\n// publishing { repositories { maven { url = uri('file:///x') } } }\nversion = '1.2.3'\n", false},
		{"empty-publishing", "plugins { id 'java'; id 'maven-publish' }\nversion = '1.2.3'\npublishing { }\n", false},
		{"configured", "plugins { id 'java'; id 'maven-publish' }\nversion = '1.2.3'\npublishing { repositories { maven { url = uri('file:///x') } } }\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := ecosystemtest.Fixture(t, "gradle-literal")
			writeFile(t, root, "build.gradle", tc.script)
			pkgs := detect(t, root)
			if len(pkgs) != 1 {
				t.Fatalf("packages = %+v", pkgs)
			}
			if pkgs[0].Publish != tc.want {
				t.Fatalf("publish = %v, want %v", pkgs[0].Publish, tc.want)
			}
		})
	}
}

func TestForeignBuildSystemIsNotScanned(t *testing.T) {
	root := ecosystemtest.Fixture(t, "mixed-build")
	scanner, ok := java.New().(ecosystem.ManifestScanner)
	if !ok {
		t.Fatal("java adapter does not implement ManifestScanner")
	}
	scan, err := scanner.ScanManifest(root, ecosystem.Package{Path: "fixtures", Manifest: "fixtures/pom.xml", Resolver: "java"})
	if err != nil {
		t.Fatalf("ScanManifest: %v", err)
	}
	if scan.OK || scan.Reason != ecosystem.ScanReasonNestedBuild {
		t.Fatalf("scan = %+v", scan)
	}
}

func TestRootForeignBuildIsIgnored(t *testing.T) {
	root := ecosystemtest.Fixture(t, "maven-external-parent")
	scanner, ok := java.New().(ecosystem.ManifestScanner)
	if !ok {
		t.Fatal("java adapter does not implement ManifestScanner")
	}
	scan, err := scanner.ScanManifest(root, ecosystem.Package{Path: ".", Manifest: "build.gradle", Resolver: "java"})
	if err != nil {
		t.Fatalf("ScanManifest: %v", err)
	}
	if scan.OK || scan.Reason != "" {
		t.Fatalf("scan = %+v", scan)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
