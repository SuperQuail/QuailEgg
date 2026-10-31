package buildinfo

import "testing"

func TestVersionOverride(t *testing.T) {
	SetVersion("v1.2.3")
	t.Cleanup(func() { SetVersion("") })

	if got := Version(); got != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", got)
	}
	if got := InstallVersion(); got != "v1.2.3" {
		t.Fatalf("InstallVersion = %q, want v1.2.3", got)
	}
}

func TestSourceBuildIsDevel(t *testing.T) {
	SetVersion("")
	t.Cleanup(func() { SetVersion("") })

	if got := Version(); got != "devel" {
		t.Fatalf("Version = %q, want devel", got)
	}
	if got := InstallVersion(); got != "latest" {
		t.Fatalf("InstallVersion = %q, want latest", got)
	}
}
