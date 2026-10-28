package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gofrs/flock"
)

// runsDirName is the local checkpoint directory below .quailegg.
const runsDirName = "state"

// Step statuses.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// StepState is one executed step.
type StepState struct {
	Status     string    `json:"status"`
	At         time.Time `json:"at,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// PackageState is the publish outcome of one package.
type PackageState struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// RunRecord is one release run.
type RunRecord struct {
	SchemaVersion int                     `json:"schema_version"`
	RunID         string                  `json:"run_id"`
	StartedAt     time.Time               `json:"started_at"`
	UpdatedAt     time.Time               `json:"updated_at"`
	Status        string                  `json:"status"`
	Mode          string                  `json:"mode"`
	Phase         string                  `json:"phase"`
	Fingerprint   string                  `json:"fingerprint"`
	Steps         map[string]StepState    `json:"steps"`
	Publish       map[string]PackageState `json:"publish,omitempty"`
	Assets        []string                `json:"assets,omitempty"`
}

// NewRunRecord creates a run record.
func NewRunRecord(runID, mode, phase, fingerprint string) *RunRecord {
	now := time.Now()
	return &RunRecord{
		SchemaVersion: 1,
		RunID:         runID,
		StartedAt:     now,
		UpdatedAt:     now,
		Status:        StatusRunning,
		Mode:          mode,
		Phase:         phase,
		Fingerprint:   fingerprint,
		Steps:         map[string]StepState{},
		Publish:       map[string]PackageState{},
	}
}

// Mark records a step status.
func (r *RunRecord) Mark(step, status string, err error) {
	r.UpdatedAt = time.Now()
	state := r.Steps[step]
	state.Status = status
	state.At = r.UpdatedAt
	if err != nil {
		state.Status = StatusFailed
		state.Error = err.Error()
	}
	r.Steps[step] = state
}

// MarkDuration records a step status plus duration.
func (r *RunRecord) MarkDuration(step, status string, d time.Duration) {
	r.Mark(step, status, nil)
	state := r.Steps[step]
	state.DurationMS = d.Milliseconds()
	r.Steps[step] = state
}

// Done reports whether a step already completed.
func (r *RunRecord) Done(step string) bool {
	state, ok := r.Steps[step]
	return ok && state.Status == StatusDone
}

// StateDir returns the state directory.
func StateDir(root string) string {
	return filepath.Join(root, ".quailegg", runsDirName)
}

// RuntimePaths lists the repository-relative metadata qegg writes while a
// release runs. The dirty check ignores them: they are local checkpoints, not
// pending user changes.
func RuntimePaths() []string {
	return []string{
		filepath.ToSlash(filepath.Join(metadataDirName, runsDirName)),
		filepath.ToSlash(filepath.Join(metadataDirName, "lock")),
		filepath.ToSlash(filepath.Join(metadataDirName, "assets")),
	}
}

// SaveRun writes the run and updates latest.json under a file lock.
func SaveRun(root string, run *RunRecord) error {
	dir := StateDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(root, ".quailegg", "lock"))
	if err := lock.Lock(); err != nil {
		return err
	}
	defer lock.Unlock()

	run.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, run.RunID+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	latest := struct {
		RunID string `json:"run_id"`
	}{RunID: run.RunID}
	latestData, err := json.MarshalIndent(latest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "latest.json"), append(latestData, '\n'), 0o644)
}

// LatestRun loads the most recent run; nil when none exists.
func LatestRun(root string) (*RunRecord, error) {
	data, err := os.ReadFile(filepath.Join(StateDir(root), "latest.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var pointer struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(data, &pointer); err != nil {
		return nil, fmt.Errorf("latest.json: %w", err)
	}
	if pointer.RunID == "" {
		return nil, nil
	}
	runData, err := os.ReadFile(filepath.Join(StateDir(root), pointer.RunID+".json"))
	if err != nil {
		return nil, err
	}
	var run RunRecord
	if err := json.Unmarshal(runData, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ListRuns returns the run ids in the state directory, newest first.
func ListRuns(root string) ([]string, error) {
	entries, err := os.ReadDir(StateDir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || entry.Name() == "latest.json" {
			continue
		}
		ids = append(ids, entry.Name()[:len(entry.Name())-5])
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids, nil
}
