package python_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/python"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

func writePyproject(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncVersionsRewritesSiblingPins(t *testing.T) {
	root := t.TempDir()
	writePyproject(t, filepath.Join(root, "packages", "core", "pyproject.toml"), "[project]\nname = \"core\"\nversion = \"1.0.1\"\n")
	app := "[project]\nname = \"app\"\nversion = \"1.0.1\"\ndependencies = [\n" +
		"  \"core==1.0.0\",\n" +
		"  \"core[x]==1.0.0; python_version < '3.12'\",\n" +
		"  \"click==8.1.0\",\n" +
		"]\n"
	appPath := filepath.Join(root, "app", "pyproject.toml")
	writePyproject(t, appPath, app)

	syncer, ok := python.New().(ecosystem.VersionSyncer)
	if !ok {
		t.Fatal("python adapter does not implement VersionSyncer")
	}
	pkgs := []ecosystem.Package{
		{Name: "core", Path: "packages/core", Resolver: "python", Version: "1.0.1"},
		{Name: "app", Path: "app", Resolver: "python", Version: "1.0.1"},
	}
	changed, err := syncer.SyncVersions(context.Background(), execx.OSRunner{}, root, pkgs)
	if err != nil {
		t.Fatalf("SyncVersions: %v", err)
	}
	if len(changed) != 1 || changed[0] != "app/pyproject.toml" {
		t.Fatalf("changed = %v", changed)
	}
	data, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"\"core==1.0.1\"", "\"core[x]==1.0.1; python_version < '3.12'\"", "\"click==8.1.0\""} {
		if !strings.Contains(text, want) {
			t.Fatalf("app manifest is missing %s:\n%s", want, text)
		}
	}
}

func TestSyncVersionsLeavesOtherPins(t *testing.T) {
	root := t.TempDir()
	manifest := "[project]\nname = \"app\"\nversion = \"1.0.0\"\ndependencies = [\n  \"other==1.0.0\",\n]\n"
	writePyproject(t, filepath.Join(root, "app", "pyproject.toml"), manifest)
	syncer := python.New().(ecosystem.VersionSyncer)
	pkgs := []ecosystem.Package{{Name: "app", Path: "app", Resolver: "python", Version: "1.0.1"}, {Name: "core", Path: "packages/core", Resolver: "python", Version: "1.0.1"}}
	changed, err := syncer.SyncVersions(context.Background(), execx.OSRunner{}, root, pkgs)
	if err != nil {
		t.Fatalf("SyncVersions: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want none", changed)
	}
}
