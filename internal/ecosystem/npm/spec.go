// Package npm adapts Node.js packages (package.json, npm and pnpm workspaces).
package npm

import (
	_ "embed"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

//go:embed workflow-setup.yml
var workflowSetup string

//go:embed workflow-env.yml
var workflowEnv string

// New returns the npm ecosystem adapter.
func New() ecosystem.Ecosystem { return driver{} }

type driver struct{}

// Spec describes the npm ecosystem.
func (driver) Spec() ecosystem.Spec {
	return ecosystem.Spec{
		ID:            "npm",
		Language:      "Node.js",
		Manifests:     []string{"package.json"},
		Publishable:   true,
		WorkflowSetup: workflowSetup,
		WorkflowEnv:   workflowEnv,
		Tools: []ecosystem.Tool{
			{Name: "npm", Command: "npm"},
			{Name: "gh", Command: "gh"},
		},
	}
}

// Publisher returns the npm registry publisher.
func (driver) Publisher() ecosystem.Publisher { return publisher{} }
