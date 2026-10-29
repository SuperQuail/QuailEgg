package release

import (
	"os"
	"testing"
)

// TestMain keeps the optional GitHub lookups off the network. Every request
// fails against a closed local port, which the author lookup treats as
// offline, so plans stay deterministic and hermetic.
func TestMain(m *testing.M) {
	os.Setenv("GITHUB_API_URL", "http://127.0.0.1:1")
	os.Exit(m.Run())
}
