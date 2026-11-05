package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
)

func TestRenderConfigChangelog(t *testing.T) {
	off := false
	out := renderConfig(Options{
		BaseBranch: "main",
		Mode:       config.ModePR,
		TagPrefix:  "v",
		Remote:     "origin",
		ChangesDir: ".changes",
		Changelog:  &off,
	}, nil)
	if !strings.Contains(string(out), "changelog = false") {
		t.Fatalf("generated config is missing changelog = false:\n%s", out)
	}
	path := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("generated config does not load: %v\n%s", err, out)
	}
	if cfg.Release.ChangelogEnabled() {
		t.Fatal("changelog = false did not survive the round trip")
	}
}

func TestRenderConfigOmitsDefaultChangelog(t *testing.T) {
	out := renderConfig(Options{
		BaseBranch: "main",
		Mode:       config.ModePR,
		TagPrefix:  "v",
		Remote:     "origin",
		ChangesDir: ".changes",
	}, nil)
	if strings.Contains(string(out), "changelog") {
		t.Fatalf("the default changelog policy must stay implicit:\n%s", out)
	}
}

func TestMergeChangelog(t *testing.T) {
	existing := []byte("# keep me\nschema_version = 1\n\n[release]\nmode = \"pr\"\ngithub_release = true\n\n[changes]\ndir = \".changes\"\n\n[release.assets]\nenabled = true\n")
	off := false
	merged := MergeChangelog(existing, &off)
	text := string(merged)
	if !strings.Contains(text, "[release]\nmode = \"pr\"\ngithub_release = true\nchangelog = false\n") {
		t.Fatalf("changelog was not inserted into the release section:\n%s", text)
	}
	if !strings.HasPrefix(text, "# keep me") {
		t.Fatalf("existing content was rewritten:\n%s", text)
	}
	if !strings.Contains(text, "[release.assets]\nenabled = true\n") {
		t.Fatalf("the release sub-tables were touched:\n%s", text)
	}
	if again := MergeChangelog(merged, &off); string(again) != text {
		t.Fatalf("merge is not idempotent:\n%s", again)
	}
	on := true
	onText := string(MergeChangelog(merged, &on))
	if !strings.Contains(onText, "changelog = true") || strings.Contains(onText, "changelog = false") {
		t.Fatalf("the switch was not turned back on:\n%s", onText)
	}
	if string(MergeChangelog(existing, nil)) != string(existing) {
		t.Fatal("a nil policy must leave the config untouched")
	}
	bare := string(MergeChangelog([]byte("schema_version = 1\n"), &off))
	if !strings.Contains(bare, "[release]\nchangelog = false") {
		t.Fatalf("a config without a release section needs one:\n%s", bare)
	}
}

// TestChangelogTriggerPath pins the generated workflow filter: CHANGELOG.md
// wakes the release run only while a release writes the file.
func TestChangelogTriggerPath(t *testing.T) {
	enabled := buildPlan(t, Options{})
	if content := string(workflowFile(t, enabled, "qegg-release.yml").Content); !strings.Contains(content, "- \"CHANGELOG.md\"") {
		t.Fatalf("the release workflow must watch CHANGELOG.md while the changelog is on:\n%s", content)
	}
	off := false
	disabled := buildPlan(t, Options{Changelog: &off})
	if content := string(workflowFile(t, disabled, "qegg-release.yml").Content); strings.Contains(content, "\"CHANGELOG.md\"") {
		t.Fatalf("a disabled changelog must not trigger a release:\n%s", content)
	}
}
