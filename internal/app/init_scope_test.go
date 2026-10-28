package app

import (
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
)

func TestDefaultScopeUsesPackageDirectories(t *testing.T) {
	packages := map[string]config.Package{
		"core": {Path: "packages/core", Resolver: config.ResolverNPM},
		"cli":  {Path: "packages/cli", Resolver: config.ResolverNPM},
		"root": {Path: ".", Resolver: config.ResolverNPM},
	}
	got := defaultScope(packages)
	want := []string{"packages/cli", "packages/core"}
	if len(got) != len(want) {
		t.Fatalf("defaultScope = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("defaultScope = %v, want %v", got, want)
		}
	}
}

func TestDefaultScopeOfRootPackageIsEmpty(t *testing.T) {
	got := defaultScope(map[string]config.Package{
		"app": {Path: ".", Resolver: config.ResolverNPM},
	})
	if len(got) != 0 {
		t.Fatalf("defaultScope = %v, want an empty scope for a root package", got)
	}
}
