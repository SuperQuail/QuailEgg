package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaults(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Release.Mode != ModePR {
		t.Errorf("mode = %q, want pr", cfg.Release.Mode)
	}
	if cfg.Git.TagPrefix != "v" || cfg.Changes.Dir != ".changes" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.Verify.Timeout != "15m" {
		t.Errorf("verify timeout = %q", cfg.Verify.Timeout)
	}
}

// A newer qegg may write keys this binary does not know yet; the old binary
// has to keep working, so unknown keys are ignored instead of rejected.
func TestUnknownFieldsAreIgnored(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[project]\ndefault_branch = \"main\"\nrepo = \"github.com/a/b\"\nfuture_key = true\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("unknown keys must not fail the load: %v", err)
	}
	if cfg.Project.DefaultBranch != "main" {
		t.Fatalf("known keys must still be read: %+v", cfg.Project)
	}
}

func TestInvalidMode(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[release]\nmode = \"fast\"\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for an invalid mode")
	}
}

func TestUnsupportedSchema(t *testing.T) {
	path := writeConfig(t, "schema_version = 2\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for an unsupported schema version")
	}
}

func TestPackagePublishable(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[packages.a]\npath = \".\"\nresolver = \"npm\"\n\n[packages.b]\npath = \"b\"\nresolver = \"npm\"\npublish = false\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !cfg.PackagePublishable("a") {
		t.Error("a should be publishable by default")
	}
	if cfg.PackagePublishable("b") {
		t.Error("b should not be publishable")
	}
}

func TestReleaseAssetsConfig(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[release.assets]\ntargets = [\"linux/amd64\", \"windows/amd64\"]\nldflags = [\"-X main.version={{version}}\"]\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(cfg.Release.Assets.Targets) != 2 || cfg.Release.Assets.Targets[1] != "windows/amd64" {
		t.Fatalf("unexpected targets: %v", cfg.Release.Assets.Targets)
	}
	if len(cfg.Release.Assets.Ldflags) != 1 {
		t.Fatalf("unexpected ldflags: %v", cfg.Release.Assets.Ldflags)
	}
}

func TestReleaseAssetsRejectBadTarget(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[release.assets]\ntargets = [\"linux\"]\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for a malformed asset target")
	}
}

func TestReleaseAssetsMainCannotEscape(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n[release.assets]\nmain = \"../outside\"\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for a main package outside the repository")
	}
}

func TestLegacyLanguageKeyAccepted(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\nlanguage = \"en\"\n")
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("legacy language key must keep loading: %v", err)
	}
}

// A shared release can cover packages from different ecosystems: a Tauri app
// keeps its Rust crates and its frontend on one version.
func TestSharedScopeAllowsMixedResolvers(t *testing.T) {
	path := writeConfig(t, `schema_version = 1

[release]
version_scope = "shared"

[packages."sc2translater-ui"]
path = "ui"
resolver = "npm"

[packages.sc2translater]
path = "src-tauri"
resolver = "rust"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Release.VersionScope != VersionScopeShared {
		t.Fatalf("version scope = %q", cfg.Release.VersionScope)
	}
}

func TestVersionFileConfig(t *testing.T) {
	path := writeConfig(t, `schema_version = 1

[packages.sc2translater]
path = "src-tauri"
resolver = "rust"

[[version.file]]
path = "src-tauri/tauri.conf.json"
format = "json"
key = "version"
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(cfg.Version.File) != 1 {
		t.Fatalf("version files = %+v", cfg.Version.File)
	}
	file := cfg.Version.File[0]
	if file.Path != "src-tauri/tauri.conf.json" || file.Format != "json" || file.Key != "version" {
		t.Fatalf("version file = %+v", file)
	}
}

func TestVersionFileNeedsSharedScopeWithSeveralPackages(t *testing.T) {
	path := writeConfig(t, `schema_version = 1

[packages.a]
path = "a"
resolver = "rust"

[packages.b]
path = "b"
resolver = "npm"

[[version.file]]
path = "app.json"
`)
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for a version file in a package scoped repository")
	}
}

func TestVersionFileCannotEscape(t *testing.T) {
	path := writeConfig(t, `schema_version = 1

[[version.file]]
path = "../outside.json"
`)
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for a version file outside the repository")
	}
}

func TestValidateRejectsReleaseBranchEqualToDefault(t *testing.T) {
	cfg := Default()
	cfg.Release.Branch = cfg.Project.DefaultBranch
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected a validation error")
	}
}

func TestAssetsCommandsAndFiles(t *testing.T) {
	path := writeConfig(t, `schema_version = 1

[packages.sc2translater]
path = "."
resolver = "rust"

[release.assets]
enabled = true
commands = ["pnpm -C ui build", "cargo tauri build"]
files = ["target/release/bundle/**/*.msi"]
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	assets := cfg.Release.Assets
	if !assets.Requested() {
		t.Fatal("commands and files must request assets")
	}
	if len(assets.Commands) != 2 || len(assets.Files) != 1 {
		t.Fatalf("assets = %+v", assets)
	}
}

func TestAssetsCommandsNeedFiles(t *testing.T) {
	path := writeConfig(t, `schema_version = 1

[release.assets]
commands = ["make release"]
`)
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for commands without files")
	}
}

func TestAssetsFilesCannotEscape(t *testing.T) {
	path := writeConfig(t, `schema_version = 1

[release.assets]
files = ["../outside.zip"]
`)
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for a glob outside the repository")
	}
}

func TestAssetsNeedSharedScopeForSeveralPackages(t *testing.T) {
	body := `schema_version = 1

[packages.a]
path = "a"
resolver = "rust"

[packages.b]
path = "b"
resolver = "npm"

[release.assets]
files = ["dist/**"]
`
	if _, err := LoadFile(writeConfig(t, body)); err == nil {
		t.Fatal("expected an error in a package scoped multi-package repository")
	}
	shared := strings.Replace(body, "[release.assets]", "[release]\nversion_scope = \"shared\"\n\n[release.assets]", 1)
	if _, err := LoadFile(writeConfig(t, shared)); err != nil {
		t.Fatalf("shared scope must allow assets: %v", err)
	}
}

func TestWorkflowConfig(t *testing.T) {
	path := writeConfig(t, `schema_version = 1

[release.workflow]
mode = "managed"
runner = "windows-latest"
timeout_minutes = 30
steps = ["pnpm -C ui install --frozen-lockfile"]

[[release.build]]
name = "windows"
runner = "windows-latest"
steps = ["pnpm -C ui build", "cargo tauri build"]
artifacts = ["target/release/bundle/msi/*.msi"]

[[release.build]]
name = "macos"
steps = ["cargo tauri build"]
artifacts = ["target/release/bundle/dmg/*.dmg"]
`)
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	workflow := cfg.Release.Workflow
	if workflow.Mode != WorkflowManaged || workflow.Runner != "windows-latest" || workflow.TimeoutMinutes != 30 {
		t.Fatalf("workflow = %+v", workflow)
	}
	if len(workflow.Steps) != 1 || len(cfg.Release.Build) != 2 {
		t.Fatalf("workflow steps or build jobs missing: %+v", cfg.Release)
	}
	mac := cfg.Release.Build[1]
	if mac.Runner != "windows-latest" || mac.TimeoutMinutes != 30 {
		t.Fatalf("build defaults were not applied: %+v", mac)
	}
}

func TestWorkflowYAMLSteps(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n\n[release.workflow]\nyaml = \"\"\"\n- uses: pnpm/action-setup@v4\n  with: { version: 11 }\n\"\"\"\n\n[[release.build]]\nname = \"macos-arm\"\nrunner = \"macos-latest\"\nyaml = \"\"\"\n- uses: actions/checkout@v4\n- uses: dtolnay/rust-toolchain@master\n  with: { toolchain: \"1.94.0\" }\n\"\"\"\nartifacts = [\"target/release/bundle/**/*\"]\n")
	cfg, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !strings.Contains(cfg.Release.Workflow.YAML, "pnpm/action-setup") {
		t.Fatalf("workflow yaml = %q", cfg.Release.Workflow.YAML)
	}
	if len(cfg.Release.Build) != 1 || !strings.Contains(cfg.Release.Build[0].YAML, "rust-toolchain") {
		t.Fatalf("build yaml = %+v", cfg.Release.Build)
	}
}

func TestWorkflowRejectsBothStepForms(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n\n[release.workflow]\nsteps = [\"echo hi\"]\nyaml = \"\"\"\n- run: echo hi\n\"\"\"\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error when steps and yaml are both set")
	}
}

func TestWorkflowEnvOnlyWithSteps(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n\n[release.workflow]\nenv = { CACHE_DIR = \".cache\" }\nyaml = \"\"\"\n- run: echo hi\n\"\"\"\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for env with a yaml step block")
	}
}

func TestBuildJobNeedsStepsOrYAML(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n\n[[release.build]]\nname = \"windows\"\nartifacts = [\"dist/*.msi\"]\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for a build job without steps")
	}
}

func TestAssetsRejectsGoSettingsWithFiles(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n\n[release.assets]\nfiles = [\"dist/*.msi\"]\nmain = \"cmd/tool\"\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for files combined with main")
	}
}

func TestBuildJobsNeedAssetsEnabled(t *testing.T) {
	path := writeConfig(t, "schema_version = 1\n\n[release.assets]\nenabled = false\n\n[[release.build]]\nname = \"windows\"\nsteps = [\"echo build\"]\nartifacts = [\"dist/*.msi\"]\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for build jobs with assets disabled")
	}
}

func TestWorkflowDefaults(t *testing.T) {
	cfg, err := LoadFile(writeConfig(t, "schema_version = 1\n"))
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Release.Workflow.Mode != WorkflowManaged || cfg.Release.Workflow.Runner != DefaultWorkflowRunner || cfg.Release.Workflow.TimeoutMinutes != 60 {
		t.Fatalf("workflow defaults = %+v", cfg.Release.Workflow)
	}
}

func TestWorkflowRejectsExternalBuildJobs(t *testing.T) {
	path := writeConfig(t, `schema_version = 1

[release.workflow]
mode = "external"

[[release.build]]
name = "windows"
artifacts = ["dist/*.msi"]
`)
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for build jobs in external mode")
	}
}

func TestWorkflowRejectsBadBuildName(t *testing.T) {
	path := writeConfig(t, `schema_version = 1

[[release.build]]
name = "windows build"
artifacts = ["dist/*.msi"]
`)
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected an error for a build job name with a space")
	}
}

func TestProjectRepoValidation(t *testing.T) {
	good := writeConfig(t, "schema_version = 1\n[project]\ndefault_branch = \"main\"\nrepo = \"github.com/SuperQuail/QuailEgg\"\n")
	if _, err := LoadFile(good); err != nil {
		t.Fatalf("a canonical repo identity must load: %v", err)
	}
	bad := writeConfig(t, "schema_version = 1\n[project]\nrepo = \"not-a-repo\"\n")
	if _, err := LoadFile(bad); err == nil {
		t.Fatal("expected an error for a repo without host/owner/name")
	}
}
