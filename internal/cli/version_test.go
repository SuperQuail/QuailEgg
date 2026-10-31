package cli

import (
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/buildinfo"
)

func TestBuildVersion(t *testing.T) {
	buildinfo.SetVersion("v1.2.3")
	t.Cleanup(func() { buildinfo.SetVersion("") })

	rt := newRuntime(&Options{})
	if got := rt.BuildVersion(); got != "1.2.3" {
		t.Fatalf("BuildVersion = %q, want 1.2.3", got)
	}
	if got := rt.InstallVersion(); got != "v1.2.3" {
		t.Fatalf("InstallVersion = %q, want v1.2.3", got)
	}
}
