package verify

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/SuperQuail/QuailEgg/internal/platform/execx"
)

// VerificationResult reports what verification did.
type VerificationResult struct {
	Commands      []string
	Skipped       bool
	Duration      time.Duration
	FailedCommand string
}

// RunVerification executes commands in order using the platform shell.
func RunVerification(ctx context.Context, run execx.Runner, root string, commands []string, timeout time.Duration, failFast bool, out io.Writer) (VerificationResult, error) {
	result := VerificationResult{Commands: commands}
	if len(commands) == 0 {
		return result, nil
	}
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	defer func() { result.Duration = time.Since(start) }()

	for _, command := range commands {
		fmt.Fprintf(out, "▶ %s\n", command)
		stepStart := time.Now()
		name, args := shellCommand(command)
		res, err := run.Run(ctx, root, name, args...)
		if res.Stdout != "" {
			fmt.Fprint(out, res.Stdout)
		}
		if res.Stderr != "" {
			fmt.Fprint(out, res.Stderr)
		}
		if err != nil {
			result.FailedCommand = command
			fmt.Fprintf(out, "✗ %s (%s)\n", command, time.Since(stepStart).Round(time.Millisecond))
			if ctx.Err() == context.DeadlineExceeded {
				return result, fmt.Errorf("verification command timed out: %s", command)
			}
			return result, fmt.Errorf("verification command failed: %s: %w", command, err)
		}
		fmt.Fprintf(out, "✓ %s (%s)\n", command, time.Since(stepStart).Round(time.Millisecond))
		_ = failFast
	}
	return result, nil
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", command}
	}
	return "sh", []string{"-c", command}
}
