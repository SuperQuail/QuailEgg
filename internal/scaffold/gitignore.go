package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// gitignoreName is the root file qegg appends its runtime entries to.
const gitignoreName = ".gitignore"

// gitignoreBlock keeps qegg's runtime state out of version control. The block
// is appended once; a repository that already ignores the state keeps its file.
const gitignoreBlock = "# qegg runtime state\n/.quailegg/state/\n/.quailegg/lock\n/.quailegg/assets/\n"

// planGitignore merges the runtime block into .gitignore instead of replacing
// it, so a hand-written file keeps every entry it had. The appended lines reuse
// the file's existing line endings.
func planGitignore(opts Options) (*File, error) {
	path := filepath.Join(opts.Root, gitignoreName)
	existing, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{Path: path, Action: ActionCreate, Content: []byte(gitignoreBlock), Perm: 0o644, Reason: "ignore qegg runtime state"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.Contains(existing, []byte("/.quailegg/state/")) {
		return &File{Path: path, Action: ActionSkip, Reason: "already ignores qegg runtime state"}, nil
	}
	eol := "\n"
	if bytes.Contains(existing, []byte("\r\n")) {
		eol = "\r\n"
	}
	block := strings.ReplaceAll(gitignoreBlock, "\n", eol)
	merged := bytes.TrimRight(existing, "\r\n")
	if len(merged) > 0 {
		merged = append(merged, []byte(eol+eol)...)
	}
	merged = append(merged, []byte(block)...)
	return &File{Path: path, Action: ActionUpdate, Content: merged, Perm: 0o644, Reason: "ignore qegg runtime state"}, nil
}
