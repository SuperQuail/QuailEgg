package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func plannedFile(t *testing.T, opts Options, name string) *File {
	t.Helper()
	plan, err := Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, file := range plan.Files {
		if filepath.Base(file.Path) == name {
			return file
		}
	}
	t.Fatalf("plan is missing %s", name)
	return nil
}

func TestGitignoreCreatedWhenMissing(t *testing.T) {
	opts := testOptions(t)
	opts.WithHook = false
	opts.WithWorkflows = false
	file := plannedFile(t, opts, gitignoreName)
	if file.Action != ActionCreate {
		t.Fatalf("action = %s, want create", file.Action)
	}
	for _, want := range []string{"/.quailegg/state/", "/.quailegg/lock", "/.quailegg/assets/"} {
		if !strings.Contains(string(file.Content), want) {
			t.Errorf("content is missing %q:\n%s", want, file.Content)
		}
	}
}

func TestGitignoreKeepsExistingEntries(t *testing.T) {
	opts := testOptions(t)
	opts.WithHook = false
	opts.WithWorkflows = false
	path := filepath.Join(opts.Root, gitignoreName)
	if err := os.WriteFile(path, []byte("/dist/\n/node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := plannedFile(t, opts, gitignoreName)
	if file.Action != ActionUpdate {
		t.Fatalf("action = %s, want update", file.Action)
	}
	content := string(file.Content)
	if !strings.HasPrefix(content, "/dist/\n/node_modules/\n") {
		t.Fatalf("existing entries were not preserved:\n%s", content)
	}
	if got := strings.Count(content, "/.quailegg/state/"); got != 1 {
		t.Fatalf("runtime block appears %d times:\n%s", got, content)
	}
}

func TestGitignoreIsIdempotent(t *testing.T) {
	opts := testOptions(t)
	opts.WithHook = false
	opts.WithWorkflows = false
	plan, err := Build(opts)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, err := Apply(plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	again := plannedFile(t, opts, gitignoreName)
	if again.Action != ActionSkip {
		t.Fatalf("second init planned %s, want skip", again.Action)
	}
	data, err := os.ReadFile(filepath.Join(opts.Root, gitignoreName))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "# qegg runtime state"); got != 1 {
		t.Fatalf("runtime block written %d times:\n%s", got, data)
	}
}

func TestGitignoreFollowsExistingLineEndings(t *testing.T) {
	opts := testOptions(t)
	opts.WithHook = false
	opts.WithWorkflows = false
	path := filepath.Join(opts.Root, gitignoreName)
	if err := os.WriteFile(path, []byte("/dist/\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := string(plannedFile(t, opts, gitignoreName).Content)
	if !strings.Contains(content, "/.quailegg/state/\r\n") {
		t.Fatalf("appended block should use CRLF:\n%q", content)
	}
}

func TestPullRequestWorkflowSkipsReleaseBranch(t *testing.T) {
	opts := testOptions(t)
	opts.ReleaseBranch = "release-1.x"
	file := plannedFile(t, opts, "qegg-pr.yml")
	if want := "github.head_ref != 'release-1.x'"; !strings.Contains(string(file.Content), want) {
		t.Fatalf("check job should skip the release branch, want %q:\n%s", want, file.Content)
	}
}

func TestReleaseWorkflowTriggersOnlyOnReleasePaths(t *testing.T) {
	opts := withEcosystem(testOptions(t), "go")
	file := plannedFile(t, opts, "qegg-release.yml")
	var doc map[string]any
	if err := yaml.Unmarshal(file.Content, &doc); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, file.Content)
	}
	trigger, ok := doc["on"].(map[string]any)
	if !ok {
		t.Fatalf("workflow has no on: block:\n%s", file.Content)
	}
	push, ok := trigger["push"].(map[string]any)
	if !ok {
		t.Fatalf("workflow has no push trigger:\n%s", file.Content)
	}
	raw, ok := push["paths"].([]any)
	if !ok {
		t.Fatalf("push trigger has no paths filter:\n%s", file.Content)
	}
	got := map[string]bool{}
	for _, item := range raw {
		got[fmt.Sprint(item)] = true
	}
	for _, want := range []string{".changes/**", ".quailegg.toml", markerPath, prereleaseLockPath, "CHANGELOG.md", "go.mod"} {
		if !got[want] {
			t.Errorf("paths filter is missing %q: %v", want, got)
		}
	}
	if _, ok := trigger["workflow_dispatch"]; !ok {
		t.Error("workflow_dispatch must stay available")
	}
}
