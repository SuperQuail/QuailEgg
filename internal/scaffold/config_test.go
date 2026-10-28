package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/config"
)

func TestRenderConfigDoesNotPinLanguage(t *testing.T) {
	out := renderConfig(Options{
		BaseBranch:     "main",
		Mode:           config.ModePR,
		TagPrefix:      "v",
		Remote:         "origin",
		Registries:     []string{"npm"},
		ChangesDir:     ".changes",
		VerifyCommands: []string{"go test ./..."},
	}, nil)
	if strings.Contains(string(out), "language") {
		t.Fatalf("generated config pins a language:\n%s", out)
	}
	path := filepath.Join(t.TempDir(), config.FileName)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err != nil {
		t.Fatalf("generated config does not load: %v\n%s", err, out)
	}
}
