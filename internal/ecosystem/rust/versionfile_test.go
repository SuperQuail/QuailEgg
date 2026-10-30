package rust

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

func writeTauriConf(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVersionFileCandidates(t *testing.T) {
	conf := "{\n  \"version\": \"0.1.0\"\n}\n"
	tests := []struct {
		name string
		pkg  string
		path string
		want []string
	}{
		{name: "workspace root", pkg: ".", path: "src-tauri/tauri.conf.json", want: []string{"src-tauri/tauri.conf.json"}},
		{name: "crate directory", pkg: "src-tauri", path: "src-tauri/tauri.conf.json", want: []string{"tauri.conf.json"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeTauriConf(t, filepath.Join(root, filepath.FromSlash(test.path)), conf)
			got := (driver{}).VersionFileCandidates(root, ecosystem.Package{Path: test.pkg, Resolver: "rust"})
			if len(got) != len(test.want) {
				t.Fatalf("candidates = %+v, want %v", got, test.want)
			}
			for i, want := range test.want {
				if got[i].Path != want || got[i].Format != "json" || got[i].Key != "version" {
					t.Fatalf("candidate %d = %+v, want %s", i, got[i], want)
				}
			}
		})
	}
}

func TestVersionFileCandidatesWithoutTauri(t *testing.T) {
	root := t.TempDir()
	if got := (driver{}).VersionFileCandidates(root, ecosystem.Package{Path: ".", Resolver: "rust"}); len(got) != 0 {
		t.Fatalf("candidates = %+v, want none", got)
	}
}
