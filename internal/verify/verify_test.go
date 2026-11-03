package verify

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// fakeRunner records the commands RunVerification executes and fails the ones
// listed in fail.
type fakeRunner struct {
	calls []string
	fail  map[string]bool
}

func (f *fakeRunner) Run(ctx context.Context, dir, name string, args ...string) (execx.Result, error) {
	command := ""
	if len(args) > 0 {
		command = args[len(args)-1]
	}
	f.calls = append(f.calls, command)
	if f.fail[command] {
		return execx.Result{ExitCode: 1}, errors.New("boom")
	}
	return execx.Result{}, nil
}

func TestRunVerificationStopsAtFirstFailure(t *testing.T) {
	runner := &fakeRunner{fail: map[string]bool{"first": true}}
	_, err := RunVerification(context.Background(), runner, t.TempDir(), []string{"first", "second"}, time.Minute, true, io.Discard)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(runner.calls) != 1 || runner.calls[0] != "first" {
		t.Fatalf("fail_fast must stop after the first failure, calls = %v", runner.calls)
	}
}

func TestRunVerificationRunsEveryCommandWithoutFailFast(t *testing.T) {
	runner := &fakeRunner{fail: map[string]bool{"first": true, "second": true}}
	var out bytes.Buffer
	result, err := RunVerification(context.Background(), runner, t.TempDir(), []string{"first", "success", "second"}, time.Minute, false, &out)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(runner.calls) != 3 {
		t.Fatalf("without fail_fast every command must run, calls = %v", runner.calls)
	}
	if result.FailedCommand != "first" {
		t.Fatalf("FailedCommand = %q, want the first failure", result.FailedCommand)
	}
	text := out.String()
	for _, want := range []string{"✗ first", "✗ second", "✓ success"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output is missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(err.Error(), "verification commands failed: first, second") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunVerificationPasses(t *testing.T) {
	runner := &fakeRunner{fail: map[string]bool{}}
	result, err := RunVerification(context.Background(), runner, t.TempDir(), []string{"first", "second"}, time.Minute, true, io.Discard)
	if err != nil {
		t.Fatalf("RunVerification: %v", err)
	}
	if len(runner.calls) != 2 || result.FailedCommand != "" {
		t.Fatalf("calls = %v, result = %+v", runner.calls, result)
	}
}

func TestRunVerificationWithoutCommands(t *testing.T) {
	runner := &fakeRunner{fail: map[string]bool{}}
	result, err := RunVerification(context.Background(), runner, t.TempDir(), nil, time.Minute, true, io.Discard)
	if err != nil {
		t.Fatalf("RunVerification: %v", err)
	}
	if len(runner.calls) != 0 || len(result.Commands) != 0 {
		t.Fatalf("calls = %v, result = %+v", runner.calls, result)
	}
}
