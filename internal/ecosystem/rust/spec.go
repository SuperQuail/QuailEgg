// Package rust adapts Rust crates (Cargo.toml, cargo workspaces).
package rust

import (
	_ "embed"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

//go:embed workflow-setup.yml
var workflowSetup string

//go:embed workflow-env.yml
var workflowEnv string

// New returns the Rust ecosystem adapter.
func New() ecosystem.Ecosystem { return driver{} }

type driver struct{}

// Spec describes the Rust ecosystem.
func (driver) Spec() ecosystem.Spec {
	return ecosystem.Spec{
		ID:            "rust",
		Language:      "Rust",
		Manifests:     []string{"Cargo.toml"},
		Publishable:   true,
		WorkflowSetup: workflowSetup,
		WorkflowEnv:   workflowEnv,
		Tools: []ecosystem.Tool{
			{Name: "cargo", Command: "cargo"},
			{Name: "gh", Command: "gh"},
		},
	}
}

// Publisher returns the crates.io publisher.
func (driver) Publisher() ecosystem.Publisher { return publisher{} }
