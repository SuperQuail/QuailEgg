package golang

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

type fakeRunner struct {
	stdout string
	err    error
	args   []string
}

func (f *fakeRunner) Run(_ context.Context, _ string, name string, args ...string) (execx.Result, error) {
	f.args = append([]string{name}, args...)
	if f.err != nil {
		return execx.Result{}, f.err
	}
	return execx.Result{Stdout: f.stdout}, nil
}

func goPackage() ecosystem.Package {
	return ecosystem.Package{Name: "example.com/demo", Path: ".", Resolver: "go", Manifest: "go.mod"}
}

func TestCurrentVersionPicksHighestTag(t *testing.T) {
	run := &fakeRunner{stdout: "v1.9.0\nv1.10.0\nv1.2.0\n"}
	version, err := New().(ecosystem.TagVersioner).CurrentVersion(context.Background(), run, ".", goPackage(), "v")
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if version != "1.10.0" {
		t.Fatalf("version = %q, want 1.10.0", version)
	}
	joined := strings.Join(run.args, " ")
	for _, want := range []string{"tag", "--merged", "HEAD", "--list", "v*"} {
		if !strings.Contains(joined, want) {
			t.Errorf("git arguments %q are missing %q", joined, want)
		}
	}
}

func TestCurrentVersionSkipsNonSemverTags(t *testing.T) {
	run := &fakeRunner{stdout: "nightly\nv1.2.3\nbroken-tag\n"}
	version, err := New().(ecosystem.TagVersioner).CurrentVersion(context.Background(), run, ".", goPackage(), "v")
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if version != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", version)
	}
}

func TestCurrentVersionWithoutTags(t *testing.T) {
	run := &fakeRunner{}
	version, err := New().(ecosystem.TagVersioner).CurrentVersion(context.Background(), run, ".", goPackage(), "v")
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if version != "0.0.0" {
		t.Fatalf("version = %q, want the 0.0.0 baseline", version)
	}
}

func TestCurrentVersionGitFailure(t *testing.T) {
	run := &fakeRunner{err: errors.New("git exploded")}
	_, err := New().(ecosystem.TagVersioner).CurrentVersion(context.Background(), run, ".", goPackage(), "v")
	if err == nil || !strings.Contains(err.Error(), "release tags") {
		t.Fatalf("expected a tag listing error, got %v", err)
	}
}

func TestCurrentVersionRejectsNonRootPackage(t *testing.T) {
	pkg := goPackage()
	pkg.Path = "sub"
	_, err := New().(ecosystem.TagVersioner).CurrentVersion(context.Background(), &fakeRunner{}, ".", pkg, "v")
	if err == nil || !strings.Contains(err.Error(), "repository root") {
		t.Fatalf("expected a root-only error, got %v", err)
	}
}

func TestCheckVersion(t *testing.T) {
	cases := []struct {
		path    string
		next    string
		wantErr string
	}{
		{path: "example.com/demo", next: "1.0.0"},
		{path: "example.com/demo", next: "1.4.2"},
		{path: "example.com/demo", next: "2.0.0", wantErr: "/v2"},
		{path: "example.com/demo/v2", next: "2.1.0"},
		{path: "example.com/demo/v2", next: "3.0.0", wantErr: "/v3"},
		{path: "example.com/demo/v2", next: "1.5.0", wantErr: "example.com/demo"},
	}
	for _, tc := range cases {
		pkg := ecosystem.Package{Name: tc.path, Path: ".", Resolver: "go"}
		err := New().(ecosystem.TagVersioner).CheckVersion(pkg, tc.next)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s@%s: unexpected error %v", tc.path, tc.next, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s@%s: error = %v, want %q", tc.path, tc.next, err, tc.wantErr)
		}
	}
}
