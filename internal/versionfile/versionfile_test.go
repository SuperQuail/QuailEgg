package versionfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile drops content at rel under a fresh directory.
func writeFile(t *testing.T, rel, content string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReadJSONVersion(t *testing.T) {
	root := writeFile(t, "src-tauri/tauri.conf.json", "{\n  \"productName\": \"demo\",\n  \"version\": \"0.1.0\",\n  \"bundle\": {\"version\": \"9.9.9\"}\n}\n")
	spec := Spec{Path: "src-tauri/tauri.conf.json"}
	got, err := spec.Read(root)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "0.1.0" {
		t.Fatalf("Read = %q, want 0.1.0", got)
	}
}

func TestSetVersionKeepsFormatting(t *testing.T) {
	original := "{\n  \"productName\": \"demo\",\n  \"version\": \"0.1.0\",\n  \"bundle\": {\"version\": \"9.9.9\"}\n}\n"
	root := writeFile(t, "tauri.conf.json", original)
	spec := Spec{Path: "tauri.conf.json"}
	if err := spec.SetVersion(root, "0.2.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "tauri.conf.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(original, "\"version\": \"0.1.0\"", "\"version\": \"0.2.0\"", 1)
	if string(data) != want {
		t.Fatalf("file = %q, want %q", data, want)
	}
}

func TestSetVersionCustomKey(t *testing.T) {
	root := writeFile(t, "app.json", "{\"name\": \"app\", \"release\": \"1.0.0\"}")
	spec := Spec{Path: "app.json", Format: FormatJSON, Key: "release"}
	if err := spec.SetVersion(root, "1.1.0"); err != nil {
		t.Fatalf("SetVersion: %v", err)
	}
	got, err := spec.Read(root)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "1.1.0" {
		t.Fatalf("Read = %q, want 1.1.0", got)
	}
}

func TestReadErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"missing file", "", "does not exist"},
		{"not an object", "[1, 2]", "top level"},
		{"missing key", "{\"name\": \"app\"}", "no string"},
		{"not a string", "{\"version\": 2}", "no string"},
		{"invalid json", "{\"version\": ", "valid JSON"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.content != "" {
				root = writeFile(t, "app.json", test.content)
			}
			_, err := (Spec{Path: "app.json"}).Read(root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Read error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSpecValidation(t *testing.T) {
	for _, path := range []string{"", "../outside.json", "/etc/passwd"} {
		if err := (Spec{Path: path}).Validate(); err == nil {
			t.Fatalf("Validate(%q) = nil, want an error", path)
		}
	}
	if err := (Spec{Path: "ok.json", Format: "yaml"}).Validate(); err == nil {
		t.Fatal("Validate accepted an unsupported format")
	}
	if err := (Spec{Path: "ok.json"}).Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got, err := (Spec{Path: "./nested/../ok.json"}).RepoPath(); err != nil || got != "ok.json" {
		t.Fatalf("RepoPath = %q, %v", got, err)
	}
}
