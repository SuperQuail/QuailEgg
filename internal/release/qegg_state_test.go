package release

import (
	"context"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/release/state"
)

func TestMarkerAndRunRecordCarryQeggInfo(t *testing.T) {
	dir := newRepo(t)
	d := deps(t, dir)
	d.QeggVersion = "1.2.3"
	d.QeggCommit = "abc1234"
	d.QeggDevelop = true

	plan, err := Build(context.Background(), d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if err := Run(context.Background(), d, plan); err != nil {
		t.Fatalf("Run: %v", err)
	}

	marker, err := state.LoadMarker(dir)
	if err != nil || marker == nil || marker.Qegg == nil {
		t.Fatalf("marker = %+v, err = %v", marker, err)
	}
	if marker.Qegg.Version != "1.2.3" || marker.Qegg.Commit != "abc1234" || !marker.Qegg.Develop {
		t.Fatalf("marker qegg = %+v", marker.Qegg)
	}

	run, err := state.LatestRun(dir)
	if err != nil || run == nil || run.Qegg == nil {
		t.Fatalf("run = %+v, err = %v", run, err)
	}
	if run.Qegg.Version != "1.2.3" || run.Qegg.Commit != "abc1234" || !run.Qegg.Develop {
		t.Fatalf("run qegg = %+v", run.Qegg)
	}
}
