package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
)

func TestRenderConfigDoesNotPinLanguage(t *testing.T) {
	out := renderConfig(Options{
		BaseBranch:     "main",
		Mode:           config.ModePR,
		TagPrefix:      "v",
		Remote:         "origin",
		Registries:     []string{"npm"},
		ChangesDir:     ".changes",
		VerifyCommands: []string{"go test ./..."},
	}, nil)
	if strings.Contains(string(out), "language") {
		t.Fatalf("generated config pins a language:\n%s", out)
	}
	path := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err != nil {
		t.Fatalf("generated config does not load: %v\n%s", err, out)
	}
}

func TestRenderConfigRecordsRepo(t *testing.T) {
	out := renderConfig(Options{
		BaseBranch: "main",
		Repo:       "github.com/example/demo",
		Mode:       config.ModePR,
		TagPrefix:  "v",
		Remote:     "origin",
		ChangesDir: ".changes",
	}, nil)
	if !strings.Contains(string(out), `repo = "github.com/example/demo"`) {
		t.Fatalf("generated config is missing the repo identity:\n%s", out)
	}
	path := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err != nil {
		t.Fatalf("generated config does not load: %v\n%s", err, out)
	}
}

func TestMergeProjectRepo(t *testing.T) {
	existing := []byte("# keep me\nschema_version = 1\n\n[project]\ndefault_branch = \"main\"\n\n[release]\nmode = \"pr\"\n")
	merged := MergeProjectRepo(existing, "github.com/example/demo")
	if !strings.Contains(string(merged), "repo = \"github.com/example/demo\"") {
		t.Fatalf("repo identity was not inserted:\n%s", merged)
	}
	if !strings.HasPrefix(string(merged), "# keep me") {
		t.Fatalf("existing content was rewritten:\n%s", merged)
	}
	again := MergeProjectRepo(merged, "github.com/example/demo")
	if string(again) != string(merged) {
		t.Fatalf("merge is not idempotent:\n%s", again)
	}
	if string(MergeProjectRepo(existing, "")) != string(existing) {
		t.Fatal("an empty identity must leave the config untouched")
	}
	bare := MergeProjectRepo([]byte("schema_version = 1\n"), "github.com/example/demo")
	if !strings.Contains(string(bare), "[project]") || !strings.Contains(string(bare), "repo = ") {
		t.Fatalf("a config without a project table needs one:\n%s", bare)
	}
}

func TestRenderConfigSharedScopeAndEnvironment(t *testing.T) {
	packages := map[string]config.Package{
		"core": {Path: "packages/core", Resolver: "python"},
		"app":  {Path: "app", Resolver: "python"},
	}
	out := renderConfig(Options{
		BaseBranch:   "main",
		Mode:         config.ModePR,
		TagPrefix:    "v",
		Remote:       "origin",
		ChangesDir:   ".changes",
		VersionScope: config.VersionScopeShared,
		Environment:  "pypi",
		Registries:   []string{"pypi"},
		Packages:     packages,
	}, packages)
	if !strings.Contains(string(out), `version_scope = "shared"`) {
		t.Fatalf("shared config is missing version_scope:\n%s", out)
	}
	if !strings.Contains(string(out), `environment = "pypi"`) {
		t.Fatalf("config is missing environment:\n%s", out)
	}
	path := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err != nil {
		t.Fatalf("generated config does not load: %v\n%s", err, out)
	}
}

func TestRenderConfigOmitsDefaultScopeAndCommitMessage(t *testing.T) {
	packages := map[string]config.Package{
		"core": {Path: "packages/core", Resolver: "npm"},
		"cli":  {Path: "packages/cli", Resolver: "npm"},
	}
	out := renderConfig(Options{
		BaseBranch: "main",
		Mode:       config.ModePR,
		TagPrefix:  "v",
		Remote:     "origin",
		ChangesDir: ".changes",
		Packages:   packages,
	}, packages)
	if strings.Contains(string(out), "version_scope") {
		t.Fatalf("package scope must stay implicit:\n%s", out)
	}
	if !strings.Contains(string(out), `commit_message = "chore(release): bump versions"`) {
		t.Fatalf("multi-package config needs a neutral commit message:\n%s", out)
	}
}

func TestRenderConfigDevelop(t *testing.T) {
	out := renderConfig(Options{
		BaseBranch: "main",
		Mode:       config.ModePR,
		TagPrefix:  "v",
		Remote:     "origin",
		ChangesDir: ".changes",
		Develop:    true,
	}, nil)
	if !strings.Contains(string(out), "develop = true") {
		t.Fatalf("generated config is missing develop:\n%s", out)
	}
	path := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("generated config does not load: %v\n%s", err, out)
	}
	if !cfg.Develop {
		t.Fatal("develop did not survive the round trip")
	}
}

func TestMergeDevelop(t *testing.T) {
	existing := []byte("# keep me\nschema_version = 1\n\n[project]\ndefault_branch = \"main\"\n\n[release]\ndevelop = false\n")
	on := MergeDevelop(existing, true)
	text := string(on)
	if !strings.Contains(text, "schema_version = 1\ndevelop = true") {
		t.Fatalf("develop was not inserted after schema_version:\n%s", text)
	}
	if !strings.HasPrefix(text, "# keep me") {
		t.Fatalf("existing content was rewritten:\n%s", text)
	}
	if !strings.Contains(text, "[release]\ndevelop = false") {
		t.Fatalf("a section key named develop was touched:\n%s", text)
	}
	if again := MergeDevelop(on, true); string(again) != text {
		t.Fatalf("merge is not idempotent:\n%s", again)
	}

	off := MergeDevelop(on, false)
	offText := string(off)
	if strings.Contains(offText, "develop = true") {
		t.Fatalf("develop was not removed:\n%s", offText)
	}
	if !strings.Contains(offText, "[release]\ndevelop = false") {
		t.Fatalf("removing the top-level flag dropped a section key:\n%s", offText)
	}
	if string(MergeDevelop(existing, false)) != string(existing) {
		t.Fatal("an absent flag must leave the config untouched")
	}
}
