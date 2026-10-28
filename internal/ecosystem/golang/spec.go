// Package golang adapts Go modules (go.mod). A Go module has no version field:
// its version is the latest release tag, so this adapter derives the current
// version from git tags and publishing means pushing the tag.
package golang

import (
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

// New returns the Go module ecosystem adapter.
func New() ecosystem.Ecosystem { return driver{} }

type driver struct{}

// Spec describes the Go ecosystem. The generated workflows already install Go
// to build qegg, so no extra setup step or registry credential is needed.
func (driver) Spec() ecosystem.Spec {
	return ecosystem.Spec{
		ID:          "go",
		Language:    "Go",
		Manifests:   []string{"go.mod"},
		Publishable: true,
		Tools: []ecosystem.Tool{
			{Name: "go", Command: "go"},
			{Name: "gh", Command: "gh"},
		},
	}
}

// Publisher returns the Go module proxy publisher.
func (driver) Publisher() ecosystem.Publisher { return publisher{} }
