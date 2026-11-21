package assets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeProvenance writes the manifest and the files it describes.
func writeProvenance(t *testing.T, dir string, provenance Provenance) {
	t.Helper()
	data, err := json.MarshalIndent(provenance, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ProvenanceFile), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProvenanceWithoutManifest(t *testing.T) {
	provenance, err := LoadProvenance(t.TempDir())
	if err != nil || provenance != nil {
		t.Fatalf("LoadProvenance = %+v, %v", provenance, err)
	}
}

func TestProvenanceVerifiesCommitAndDigest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tar.gz"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	digest, err := fileSHA256(filepath.Join(dir, "app.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	commit := "0123456789abcdef0123456789abcdef01234567"

	writeProvenance(t, dir, Provenance{SchemaVersion: 1, SourceCommit: commit, SHA256: map[string]string{"app.tar.gz": digest}})
	provenance, err := LoadProvenance(dir)
	if err != nil || provenance == nil {
		t.Fatalf("LoadProvenance = %+v, %v", provenance, err)
	}
	if err := provenance.Verify(dir, commit); err != nil {
		t.Fatalf("a matching manifest must verify: %v", err)
	}
	// An abbreviated commit still names the same release.
	if err := provenance.Verify(dir, commit[:7]); err != nil {
		t.Fatalf("an abbreviated commit must verify: %v", err)
	}

	// Another commit is another release: the assets came from somewhere else.
	if err := provenance.Verify(dir, "89abcdef0123456789abcdef0123456789abcdef"); err == nil {
		t.Fatal("assets built from another commit must be refused")
	}

	// A digest that does not match the file is corruption, not a detail.
	writeProvenance(t, dir, Provenance{SchemaVersion: 1, SourceCommit: commit, SHA256: map[string]string{"app.tar.gz": "deadbeef"}})
	provenance, err = LoadProvenance(dir)
	if err != nil || provenance == nil {
		t.Fatalf("LoadProvenance = %+v, %v", provenance, err)
	}
	if err := provenance.Verify(dir, commit); err == nil {
		t.Fatal("a wrong digest must be refused")
	}
}

func TestLoadProvenanceRejectsBrokenManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ProvenanceFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProvenance(dir); err == nil {
		t.Fatal("a broken manifest must be an error")
	}
}

// TestProvenanceRequiresDigests pins the second half of the check: naming the
// right commit is not enough when the files themselves are not covered.
func TestProvenanceRequiresDigests(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tar.gz"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	commit := "0123456789abcdef0123456789abcdef01234567"

	writeProvenance(t, dir, Provenance{SchemaVersion: 1, SourceCommit: commit})
	provenance, err := LoadProvenance(dir)
	if err != nil || provenance == nil {
		t.Fatalf("LoadProvenance = %+v, %v", provenance, err)
	}
	if err := provenance.Verify(dir, commit); err == nil {
		t.Fatal("a manifest without digests must be refused")
	}

	digest, err := fileSHA256(filepath.Join(dir, "app.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	writeProvenance(t, dir, Provenance{
		SchemaVersion: 1, SourceCommit: commit,
		SHA256: map[string]string{"app.tar.gz": digest, "missing.tar.gz": digest},
	})
	provenance, err = LoadProvenance(dir)
	if err != nil || provenance == nil {
		t.Fatalf("LoadProvenance = %+v, %v", provenance, err)
	}
	if err := provenance.Verify(dir, commit); err != nil {
		t.Fatalf("a complete manifest must verify: %v", err)
	}
}

// TestProvenanceRefusesAnEmptyDirectory keeps a directory with no payload from
// looking like a verified asset set.
func TestProvenanceRefusesAnEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	writeProvenance(t, dir, Provenance{SchemaVersion: 1, SourceCommit: "abc", SHA256: map[string]string{"app.tar.gz": "x"}})
	provenance, err := LoadProvenance(dir)
	if err != nil || provenance == nil {
		t.Fatalf("LoadProvenance = %+v, %v", provenance, err)
	}
	if err := provenance.Verify(dir, "abc"); err == nil {
		t.Fatal("a directory without payload must be refused")
	}
}

// TestCollectSkipsTheProvenanceManifest keeps the manifest out of the upload:
// it describes the directory, it is not a release asset.
func TestCollectSkipsTheProvenanceManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tar.gz"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeProvenance(t, dir, Provenance{SchemaVersion: 1, SourceCommit: "abc"})
	outDir := filepath.Join(t.TempDir(), "assets")
	files, err := Collect(dir, outDir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, file := range files {
		if filepath.Base(file) == ProvenanceFile {
			t.Fatalf("the manifest must not be collected: %v", files)
		}
	}
	if len(files) != 2 {
		t.Fatalf("expected the payload and checksums, got %v", files)
	}
}
