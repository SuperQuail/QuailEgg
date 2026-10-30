package app

import (
	"context"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
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
