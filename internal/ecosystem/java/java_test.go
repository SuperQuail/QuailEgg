package java

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

const mavenParent = `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0">
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>demo</artifactId>
  <version>1.2.3</version>
  <packaging>pom</packaging>
  <modules>
    <module>core</module>
  </modules>
  <distributionManagement>
    <repository>
      <id>central</id>
      <url>https://repo.example.test/releases</url>
    </repository>
  </distributionManagement>
</project>
`

const mavenChild = `<project>
  <parent>
    <groupId>com.example</groupId>
    <artifactId>demo</artifactId>
    <version>1.2.3</version>
  </parent>
  <artifactId>core</artifactId>
</project>
`

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

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func byPath(packages []ecosystem.Package) map[string]ecosystem.Package {
	out := make(map[string]ecosystem.Package, len(packages))
	for _, pkg := range packages {
		out[pkg.Path] = pkg
	}
	return out
}

func TestSpec(t *testing.T) {
	spec := (driver{}).Spec()
	if spec.ID != "java" || spec.Language != "Java" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	if len(spec.PublishTargets) != 1 || spec.PublishTargets[0] != "maven" {
		t.Fatalf("publish targets = %v", spec.PublishTargets)
	}
	if !strings.Contains(strings.Join(spec.Manifests, ","), "pom.xml") {
		t.Fatalf("manifests = %v", spec.Manifests)
	}
	if (driver{}).Publisher().Name() != "java" {
		t.Fatal("unexpected publisher name")
	}
}

func TestDetectMaven(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pom.xml", mavenParent)
	writeFile(t, root, "core/pom.xml", mavenChild)

	packages, err := (driver{}).Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	byName := byPath(packages)
	parent, ok := byName["."]
	if !ok {
		t.Fatalf("root package missing: %+v", packages)
	}
	if parent.Name != "demo" || parent.Version != "1.2.3" || parent.Inherited {
		t.Fatalf("unexpected root package: %+v", parent)
	}
	if !parent.Publish {
		t.Fatal("a project with distributionManagement must publish")
	}
	child, ok := byName["core"]
	if !ok {
		t.Fatalf("module missing: %+v", packages)
	}
	if child.Version != "1.2.3" || !child.Inherited {
		t.Fatalf("unexpected module package: %+v", child)
	}
	if child.Manifest != "core/pom.xml" {
		t.Fatalf("manifest = %q", child.Manifest)
	}
}

func TestDetectGradle(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "settings.gradle", "rootProject.name = 'ender_infection'\ninclude 'core'\n")
	writeFile(t, root, "build.gradle", "plugins { id 'maven-publish' }\nversion = mod_version\ngroup = 'dev.example'\n")
	writeFile(t, root, "gradle.properties", "mod_version=0.2.2\n")
	writeFile(t, root, "core/build.gradle", "version = '0.5.0'\n")

	packages, err := (driver{}).Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	byName := byPath(packages)
	parent, ok := byName["."]
	if !ok {
		t.Fatalf("root package missing: %+v", packages)
	}
	if parent.Name != "ender_infection" || parent.Version != "0.2.2" {
		t.Fatalf("unexpected root package: %+v", parent)
	}
	if !parent.Publish {
		t.Fatal("maven-publish must enable publishing")
	}
	child, ok := byName["core"]
	if !ok || child.Version != "0.5.0" || child.Publish {
		t.Fatalf("unexpected subproject: %+v", packages)
	}
}

func TestSetMavenVersionAndSync(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pom.xml", mavenParent)
	writeFile(t, root, "core/pom.xml", mavenChild)

	parent := ecosystem.Package{Name: "demo", Path: ".", Resolver: "java", Version: "1.2.3", Manifest: "pom.xml"}
	if err := (driver{}).SetVersion(root, parent, "2.0.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	if text := readFile(t, root, "pom.xml"); !strings.Contains(text, "<version>2.0.0</version>") {
		t.Fatalf("root version not written:\n%s", text)
	}
	if text := readFile(t, root, "core/pom.xml"); !strings.Contains(text, "<version>1.2.3</version>") {
		t.Fatalf("child manifest changed unexpectedly:\n%s", text)
	}

	child := ecosystem.Package{Name: "core", Path: "core", Resolver: "java", Version: "2.0.0", Inherited: true, Manifest: "core/pom.xml"}
	changed, err := (driver{}).SyncVersions(context.Background(), nil, root, []ecosystem.Package{parent, child})
	if err != nil {
		t.Fatalf("SyncVersions: %v", err)
	}
	if len(changed) != 1 || changed[0] != "core/pom.xml" {
		t.Fatalf("changed = %v", changed)
	}
	if text := readFile(t, root, "core/pom.xml"); !strings.Contains(text, "<version>2.0.0</version>") {
		t.Fatalf("parent reference not synced:\n%s", text)
	}
}

func TestSetGradleVersion(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "build.gradle", "version = '0.2.2'\n")

	pkg := ecosystem.Package{Name: "demo", Path: ".", Resolver: "java", Version: "0.2.2", Manifest: "build.gradle"}
	if err := (driver{}).SetVersion(root, pkg, "0.3.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	if text := readFile(t, root, "build.gradle"); !strings.Contains(text, "version = '0.3.0'") {
		t.Fatalf("gradle version not written:\n%s", text)
	}
}

func TestSetGradlePropertyVersion(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "build.gradle", "version = mod_version\n")
	writeFile(t, root, "gradle.properties", "# build settings\nmod_version=0.2.2\n")

	pkg := ecosystem.Package{Name: "demo", Path: ".", Resolver: "java", Version: "0.2.2", Manifest: "build.gradle"}
	if err := (driver{}).SetVersion(root, pkg, "0.4.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	if text := readFile(t, root, "gradle.properties"); !strings.Contains(text, "mod_version=0.4.0") {
		t.Fatalf("property not updated:\n%s", text)
	}
}

func TestInspectAndScan(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "pom.xml", mavenParent)
	writeFile(t, root, "core/pom.xml", mavenChild)

	inspected, err := (driver{}).Inspect(root, ecosystem.Package{Name: "demo", Path: ".", Resolver: "java"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if inspected.Version != "1.2.3" || inspected.Manifest != "pom.xml" {
		t.Fatalf("unexpected inspect result: %+v", inspected)
	}

	scan, err := (driver{}).ScanManifest(root, ecosystem.Package{Path: "core", Manifest: "core/pom.xml", Resolver: "java"})
	if err != nil {
		t.Fatalf("ScanManifest: %v", err)
	}
	if !scan.OK || scan.Package.Name != "core" || !scan.Package.Inherited {
		t.Fatalf("unexpected scan result: %+v", scan)
	}
}

func TestPrecheckDoesNotGuess(t *testing.T) {
	status, err := (publisher{}).Precheck(context.Background(), nil, ecosystem.Package{Name: "demo", Resolver: "java"}, "1.0.0")
	if err != nil || status != ecosystem.Unknown {
		t.Fatalf("precheck = %v, %v", status, err)
	}
}
