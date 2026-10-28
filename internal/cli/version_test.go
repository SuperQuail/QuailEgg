package cli

import "testing"

func TestBuildVersionOverride(t *testing.T) {
	previous := BuildVersionOverride
	BuildVersionOverride = "v1.2.3"
	defer func() { BuildVersionOverride = previous }()

	rt := newRuntime(&Options{})
	if got := rt.BuildVersion(); got != "1.2.3" {
		t.Fatalf("BuildVersion = %q, want 1.2.3", got)
	}
	if got := rt.InstallVersion(); got != "v1.2.3" {
		t.Fatalf("InstallVersion = %q, want v1.2.3", got)
	}
}
