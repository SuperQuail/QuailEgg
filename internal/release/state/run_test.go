package state

import "testing"

func TestSaveAndLoad(t *testing.T) {
	root := t.TempDir()
	run := NewRunRecord("run-1", "pr", "version", "sha256:abc")
	run.Mark("preflight", StatusDone, nil)
	if err := SaveRun(root, run); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LatestRun(root)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if loaded == nil || loaded.RunID != "run-1" {
		t.Fatalf("unexpected run: %+v", loaded)
	}
	if !loaded.Done("preflight") {
		t.Fatal("preflight should be done")
	}
	if loaded.Done("publish") {
		t.Fatal("publish should not be done")
	}
}

func TestMarkFailed(t *testing.T) {
	run := NewRunRecord("run-2", "commit", "publish", "")
	run.Mark("publish", StatusFailed, errString("boom"))
	if run.Steps["publish"].Error != "boom" {
		t.Fatalf("unexpected step: %+v", run.Steps["publish"])
	}
}

type errString string

func (e errString) Error() string { return string(e) }
