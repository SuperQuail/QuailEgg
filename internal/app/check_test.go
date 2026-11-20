package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
	"github.com/SuperQuail/QuailEgg/internal/platform/ui"
)

func TestCheckReportsUnknownCapabilities(t *testing.T) {
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{
		"bogus-pkg": {Path: ".", Resolver: "bogus"},
	}
	cfg.Publish.Registries = []string{"bogus"}
	a := &App{Root: t.TempDir(), Cfg: cfg, Run: execx.OSRunner{}}
	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	codes := map[string]bool{}
	for _, problem := range report.Problems {
		codes[problem.Code] = true
	}
	if !codes["unknown-resolver"] {
		t.Fatalf("missing unknown-resolver problem: %+v", report.Problems)
	}
	if !codes["unknown-registry"] {
		t.Fatalf("missing unknown-registry problem: %+v", report.Problems)
	}
}

func writeRepoFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A version file that lags behind the manifest is a release blocker: the
// published app would advertise a different version than its packages.
func TestCheckReportsVersionFileDrift(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, "Cargo.toml", "[package]\nname = \"sc2translater\"\nversion = \"0.1.0\"\n")
	writeRepoFile(t, dir, "src-tauri/tauri.conf.json", "{\n  \"version\": \"0.0.9\"\n}\n")
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{"sc2translater": {Path: ".", Resolver: "rust"}}
	cfg.Version.File = []config.VersionFile{{Path: "src-tauri/tauri.conf.json", Format: "json", Key: "version"}}
	a := &App{Root: dir, Cfg: cfg, Run: execx.OSRunner{}}
	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !hasProblem(report, "version-file-drift") {
		t.Fatalf("missing version-file-drift problem: %+v", report.Problems)
	}
	writeRepoFile(t, dir, "src-tauri/tauri.conf.json", "{\n  \"version\": \"0.1.0\"\n}\n")
	report, err = a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if hasProblem(report, "version-file-drift") {
		t.Fatalf("an aligned version file still reports drift: %+v", report.Problems)
	}
	cfg.Version.File = []config.VersionFile{{Path: "src-tauri/missing.json", Format: "json", Key: "version"}}
	report, err = a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !hasProblem(report, "version-file-invalid") {
		t.Fatalf("missing version-file-invalid problem: %+v", report.Problems)
	}
}

// countProblems counts the problems with one code.
func countProblems(report *CheckReport, code string) int {
	count := 0
	for _, problem := range report.Problems {
		if problem.Code == code {
			count++
		}
	}
	return count
}

func hasProblem(report *CheckReport, code string) bool {
	for _, problem := range report.Problems {
		if problem.Code == code {
			return true
		}
	}
	return false
}

// A package-scoped release tags every package separately, so a package whose
// version lives in git tags cannot be planned next to another one; the shared
// scope is the only working configuration.
func TestCheckReportsPackageScopeTagVersioned(t *testing.T) {
	dir := newInitRepo(t)
	writeRepoFile(t, dir, "go.mod", "module example.com/app\n\ngo 1.26.0\n")
	writeRepoFile(t, dir, "frontend/package.json", "{\n  \"name\": \"frontend\",\n  \"private\": true,\n  \"version\": \"0.0.0\"\n}\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-qm", "chore: manifests")

	publish := false
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{
		"example.com/app": {Path: ".", Resolver: "go"},
		"frontend":        {Path: "frontend", Resolver: "npm", Publish: &publish},
	}
	a := &App{Root: dir, Cfg: cfg, Run: execx.OSRunner{}}
	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !hasProblem(report, "package-tag-versioned") {
		t.Fatalf("missing package-tag-versioned problem: %+v", report.Problems)
	}

	cfg.Release.VersionScope = config.VersionScopeShared
	report, err = a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if hasProblem(report, "package-tag-versioned") {
		t.Fatalf("the shared scope still reports a tag-versioned problem: %+v", report.Problems)
	}
	if hasProblem(report, "shared-version-mismatch") {
		t.Fatalf("aligned versions report a mismatch: %+v", report.Problems)
	}
}

func TestCheckReportsInspectFailure(t *testing.T) {
	cfg := config.Default()
	cfg.Packages = map[string]config.Package{
		"ui-pkg": {Path: "ui", Resolver: "npm"},
	}
	a := &App{Root: t.TempDir(), Cfg: cfg, Run: execx.OSRunner{}}
	report, err := a.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	codes := map[string]bool{}
	for _, problem := range report.Problems {
		codes[problem.Code] = true
	}
	if !codes["inspect-failed"] {
		t.Fatalf("missing inspect-failed problem: %+v", report.Problems)
	}
}

// --- change records in CI ---------------------------------------------------

// ciCheckRepo builds a committed repository that carries a valid record, so a
// change can keep it, replace it or remove it. It returns the root and the base
// commit of the pull request.
func ciCheckRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := hookProjectRepo(t, "https://github.com/example/demo", "github.com/example/demo")
	commitHookRecord(t, dir, "base.md", hookValidRecord)
	return dir, hookHead(t, dir)
}

// ciReport runs qegg check over dir with the CI flag the test asks for.
func ciReport(t *testing.T, dir string, ci bool) *CheckReport {
	t.Helper()
	a, err := New(dir, "", ui.Discard())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	report, err := a.Check(context.Background(), ci)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	return report
}

// setCIEnv points the GitHub Actions environment at an event file. An empty path
// means the run has no event to read.
func setCIEnv(t *testing.T, name, path string) {
	t.Helper()
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_EVENT_NAME", name)
	t.Setenv("GITHUB_EVENT_PATH", path)
}

// useCIEvent writes an event payload and points the environment at it.
func useCIEvent(t *testing.T, name, payload string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	setCIEnv(t, name, path)
}

// ciSourceChange writes a scoped source file the rule watches.
func ciSourceChange(t *testing.T, dir, marker string) {
	t.Helper()
	writeHookFile(t, filepath.Join(dir, "cmd", "main.go"), "package main\n\nvar marker = \""+marker+"\"\n")
}

// commitCIChange commits the working tree onto branch and returns the commit.
func commitCIChange(t *testing.T, dir, branch, message string) string {
	t.Helper()
	hookGit(t, dir, "checkout", "-q", "-B", branch)
	hookGit(t, dir, "add", "-A")
	hookGit(t, dir, "commit", "-qm", message)
	return hookHead(t, dir)
}

func ciPullRequestEvent(base, head string) string {
	return `{"pull_request":{"base":{"sha":"` + base + `"},"head":{"sha":"` + head + `"}}}`
}

func ciPushEvent(ref, before, after string) string {
	return `{"ref":"` + ref + `","before":"` + before + `","after":"` + after + `"}`
}

func ciMergeGroupEvent(base, head string) string {
	return `{"merge_group":{"base_sha":"` + base + `","head_sha":"` + head + `"}}`
}

// A pull request that only removes the record it inherited must be rejected: the
// record has to exist in the tree the pull request would land.
func TestCheckCIRejectsAChangeThatOnlyDeletesARecord(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir, base := ciCheckRepo(t)
	hookGit(t, dir, "checkout", "-q", "-B", "feature")
	ciSourceChange(t, dir, "feature")
	hookGit(t, dir, "rm", "-q", ".changes/base.md")
	head := commitCIChange(t, dir, "feature", "feat: change source and drop the record")
	useCIEvent(t, eventPullRequest, ciPullRequestEvent(base, head))

	report := ciReport(t, dir, true)
	if !hasProblem(report, "missing-record") {
		t.Fatalf("CI accepted a pull request that only deletes a record: %+v", report.Problems)
	}
}

// The same source change passes once the pull request carries a valid record.
func TestCheckCIAcceptsANewRecordForADeletedOne(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir, base := ciCheckRepo(t)
	hookGit(t, dir, "checkout", "-q", "-B", "feature")
	ciSourceChange(t, dir, "feature")
	hookGit(t, dir, "rm", "-q", ".changes/base.md")
	writeHookFile(t, filepath.Join(dir, ".changes", "feature.md"), hookValidRecord)
	head := commitCIChange(t, dir, "feature", "feat: change source and add a record")
	useCIEvent(t, eventPullRequest, ciPullRequestEvent(base, head))

	report := ciReport(t, dir, true)
	if hasProblem(report, "missing-record") || hasProblem(report, "record-invalid") {
		t.Fatalf("CI rejected a pull request with a valid record: %+v", report.Problems)
	}
	if report.CI == nil || len(report.CI.Records) != 1 {
		t.Fatalf("the report did not name the record it accepted: %+v", report.CI)
	}
}

// A record outside the record directory reaches the changelog never, so it must
// not satisfy the rule, exactly like the hook.
func TestCheckCIRejectsANestedRecord(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir, base := ciCheckRepo(t)
	hookGit(t, dir, "checkout", "-q", "-B", "feature")
	ciSourceChange(t, dir, "feature")
	writeHookFile(t, filepath.Join(dir, ".changes", "nested", "deep.md"), hookValidRecord)
	head := commitCIChange(t, dir, "feature", "feat: change source and add a nested record")
	useCIEvent(t, eventPullRequest, ciPullRequestEvent(base, head))

	report := ciReport(t, dir, true)
	if !hasProblem(report, "missing-record") {
		t.Fatalf("a nested record must not satisfy the rule: %+v", report.Problems)
	}
}

// The verdict follows the head the event names, not the commit this run happens
// to have checked out: the same checkout passes for one head and fails for the
// other.
func TestCheckCIFollowsThePullRequestHead(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir, base := ciCheckRepo(t)
	hookGit(t, dir, "checkout", "-q", "-B", "with-record")
	ciSourceChange(t, dir, "with-record")
	writeHookFile(t, filepath.Join(dir, ".changes", "feature.md"), hookValidRecord)
	withRecord := commitCIChange(t, dir, "with-record", "feat: change source and add a record")
	hookGit(t, dir, "checkout", "-q", base)
	ciSourceChange(t, dir, "without-record")
	withoutRecord := commitCIChange(t, dir, "without-record", "feat: change source")

	// The run checks out the head without a record while the event names the one
	// with a record.
	useCIEvent(t, eventPullRequest, ciPullRequestEvent(base, withRecord))
	report := ciReport(t, dir, true)
	if hasProblem(report, "missing-record") {
		t.Fatalf("CI judged the checked out commit instead of the event head: %+v", report.Problems)
	}

	useCIEvent(t, eventPullRequest, ciPullRequestEvent(base, withoutRecord))
	report = ciReport(t, dir, true)
	if !hasProblem(report, "missing-record") {
		t.Fatalf("the verdict did not follow the event head: %+v", report.Problems)
	}
}

// The record the rule validates is the one in the event head, not the checked out
// copy the working tree pass already read.
func TestCheckCIValidatesTheRecordsOfTheEventHead(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir, base := ciCheckRepo(t)
	hookGit(t, dir, "checkout", "-q", "-B", "good")
	ciSourceChange(t, dir, "good")
	writeHookFile(t, filepath.Join(dir, ".changes", "feature.md"), hookValidRecord)
	good := commitCIChange(t, dir, "good", "feat: change source and add a record")
	hookGit(t, dir, "checkout", "-q", base)
	ciSourceChange(t, dir, "bad")
	writeHookFile(t, filepath.Join(dir, ".changes", "feature.md"), hookBrokenRecord)
	bad := commitCIChange(t, dir, "bad", "feat: change source and break the record")
	// Check out the head with the valid record; the event names the broken one.
	hookGit(t, dir, "checkout", "-q", good)

	useCIEvent(t, eventPullRequest, ciPullRequestEvent(base, bad))
	report := ciReport(t, dir, true)
	if countProblems(report, "record-invalid") != 1 {
		t.Fatalf("want one record-invalid problem for the event head, got %+v", report.Problems)
	}

	useCIEvent(t, eventPullRequest, ciPullRequestEvent(base, good))
	report = ciReport(t, dir, true)
	if hasProblem(report, "record-invalid") {
		t.Fatalf("CI rejected the record of a valid event head: %+v", report.Problems)
	}

	// The working tree and the event head both judge the same broken record when
	// the run is checked out at the head; the report must name it once.
	hookGit(t, dir, "checkout", "-q", bad)
	useCIEvent(t, eventPullRequest, ciPullRequestEvent(base, bad))
	if report := ciReport(t, dir, true); countProblems(report, "record-invalid") != 1 {
		t.Fatalf("one broken record must be reported once: %+v", report.Problems)
	}
}

// A push and a merge group carry their own revisions; neither may be reported as
// a pull request without base and head fields.
func TestCheckCIReadsPushAndMergeGroupEvents(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir, base := ciCheckRepo(t)
	hookGit(t, dir, "checkout", "-q", "-B", "main")
	ciSourceChange(t, dir, "push")
	head := commitCIChange(t, dir, "main", "feat: change source")

	for _, tc := range []struct {
		name    string
		event   string
		payload string
	}{
		{"push", eventPush, ciPushEvent("refs/heads/main", base, head)},
		{"merge group", eventMergeGroup, ciMergeGroupEvent(base, head)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useCIEvent(t, tc.event, tc.payload)
			report := ciReport(t, dir, true)
			if hasProblem(report, "ci-event-unusable") {
				t.Fatalf("%s was read as a broken event: %+v", tc.name, report.Problems)
			}
			if !hasProblem(report, "missing-record") {
				t.Fatalf("the source change of the %s event needs a record: %+v", tc.name, report.Problems)
			}
		})
	}
}

// Legitimate pushes the check has nothing to compare stay successful, and the
// report says why instead of passing silently.
func TestCheckCISkipsPushesWithoutBranchContent(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir, _ := ciCheckRepo(t)
	hookGit(t, dir, "checkout", "-q", "-B", "main")
	ciSourceChange(t, dir, "skip")
	head := commitCIChange(t, dir, "main", "feat: change source")

	zeros := strings.Repeat("0", 40)
	for _, tc := range []struct {
		name    string
		payload string
		skipped string
	}{
		{"tag push", ciPushEvent("refs/tags/v1.0.0", zeros, head), ciSkipTagPush},
		{"removed branch", ciPushEvent("refs/heads/gone", head, zeros), ciSkipDeletedBranch},
		{"created branch", ciPushEvent("refs/heads/fresh", zeros, head), ciSkipNewBranch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useCIEvent(t, eventPush, tc.payload)
			report := ciReport(t, dir, true)
			if report.CI == nil || report.CI.Skipped != tc.skipped {
				t.Fatalf("want a %q skip, got %+v", tc.skipped, report.CI)
			}
			if hasProblem(report, "missing-record") || hasProblem(report, "ci-event-unusable") {
				t.Fatalf("a legitimate push must not fail the check: %+v", report.Problems)
			}
		})
	}
}

// An event whose revisions share no history names that, instead of reporting a
// diff that could not be computed for no stated reason.
func TestCheckCIReportsRevisionsWithoutCommonHistory(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir, base := ciCheckRepo(t)
	hookGit(t, dir, "checkout", "-q", "--orphan", "orphan")
	ciSourceChange(t, dir, "orphan")
	orphan := commitCIChange(t, dir, "orphan", "feat: start an unrelated history")
	useCIEvent(t, eventPullRequest, ciPullRequestEvent(base, orphan))

	if report := ciReport(t, dir, true); !hasProblem(report, "ci-no-common-history") {
		t.Fatalf("want a no common history problem: %+v", report.Problems)
	}
}

// An event the check cannot read is an error, never a silent pass.
func TestCheckCIReportsAnUnusableEvent(t *testing.T) {
	dir, _ := ciCheckRepo(t)
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T)
	}{
		{"missing event path", func(t *testing.T) { setCIEnv(t, "", "") }},
		{"unreadable event file", func(t *testing.T) {
			setCIEnv(t, eventPullRequest, filepath.Join(t.TempDir(), "missing.json"))
		}},
		{"undecodable payload", func(t *testing.T) { useCIEvent(t, eventPullRequest, "{not json") }},
		{"pull request without revisions", func(t *testing.T) {
			useCIEvent(t, eventPullRequest, `{"pull_request":{"base":{},"head":{}}}`)
		}},
		{"unsupported event", func(t *testing.T) { useCIEvent(t, "workflow_dispatch", "{}") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t)
			if report := ciReport(t, dir, true); !hasProblem(report, "ci-event-unusable") {
				t.Fatalf("an unusable event must fail the check: %+v", report.Problems)
			}
		})
	}
}

// A diff that cannot be computed is an error, not "nothing to check".
func TestCheckCIReportsAFailedDiff(t *testing.T) {
	dir, base := ciCheckRepo(t)
	useCIEvent(t, eventPullRequest, ciPullRequestEvent(base, "0123456789012345678901234567890123456789"))
	if report := ciReport(t, dir, true); !hasProblem(report, "ci-diff-failed") {
		t.Fatalf("a failed diff must fail the check: %+v", report.Problems)
	}
}

// Outside GitHub Actions there is no event to read; the run says so instead of
// passing silently, in the output and in the JSON report.
func TestCheckCISaysWhenItSkipsLocally(t *testing.T) {
	if err := i18n.Init("en"); err != nil {
		t.Fatalf("i18n.Init: %v", err)
	}
	dir, _ := ciCheckRepo(t)
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("GITHUB_EVENT_PATH", "")
	var out bytes.Buffer
	a, err := New(dir, "", &ui.Printer{Out: &out, Err: &out})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	report, err := a.Check(context.Background(), true)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if hasProblem(report, "ci-event-unusable") || hasProblem(report, "missing-record") {
		t.Fatalf("a local run must not report a CI problem: %+v", report.Problems)
	}
	// Assert the English sentence itself: comparing against the catalog would pass
	// even when the key is missing and i18n echoes the key back.
	if !strings.Contains(out.String(), "no GitHub event to read") {
		t.Fatalf("the local run did not say it skipped the pull request diff: %s", out.String())
	}
	var decoded struct {
		OK bool `json:"ok"`
		CI *struct {
			Skipped string `json:"skipped"`
		} `json:"ci"`
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal the report: %v", err)
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal the report: %v", err)
	}
	if decoded.CI == nil || decoded.CI.Skipped != ciSkipNoEvent {
		t.Fatalf("the skipped check is invisible in the JSON report: %s", raw)
	}
}
