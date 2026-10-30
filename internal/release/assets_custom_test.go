package release

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
)

// TestSharedCustomAssetsRelease covers a Tauri-style repository: one shared
// version across a Cargo workspace and a frontend, with the release attaching
// files that a custom build produced.
func TestSharedCustomAssetsRelease(t *testing.T) {
	dir := newTauriRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "SC2Translater_0.1.0_x64_en-US.msi"), []byte("bundle"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-qm", "test: add a bundle")

	d := tauriDeps(t, dir)
	enabled := true
	d.Cfg.Release.Assets = config.Assets{Enabled: &enabled, Files: []string{"dist/*.msi"}}
	ctx := context.Background()

	plan, err := Build(ctx, d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if plan.Assets == nil {
		t.Fatal("the plan has no assets")
	}
	if len(plan.Assets.Files) != 1 || len(plan.Assets.Targets) != 0 {
		t.Fatalf("unexpected asset plan: %+v", plan.Assets)
	}
	if plan.Version.Next != "0.1.1" {
		t.Fatalf("version next = %q", plan.Version.Next)
	}

	if err := Run(ctx, d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	outDir := filepath.Join(dir, ".quailegg", "assets", "v0.1.1")
	for _, name := range []string{"SC2Translater_0.1.0_x64_en-US.msi", "checksums.txt"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("asset %s was not collected: %v", name, err)
		}
	}
}
