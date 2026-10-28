package scaffold

import (
	"os"

	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// Apply writes every non-skipped file.
func Apply(plan *Plan) ([]string, error) {
	var wrote []string
	for _, dir := range plan.Dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	for _, file := range plan.Files {
		if file.Action == ActionSkip {
			continue
		}
		if err := fsx.WriteFileAtomic(file.Path, file.Content, file.Perm); err != nil {
			return nil, err
		}
		wrote = append(wrote, file.Path)
	}
	return wrote, nil
}
