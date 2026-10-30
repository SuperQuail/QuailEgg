// Package python adapts Python packages (pyproject.toml, PEP 621, Poetry,
// hatch and setuptools dynamic versions, uv workspaces).
package python

import (
	_ "embed"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

//go:embed workflow-setup.yml
var workflowSetup string

//go:embed workflow-env.yml
var workflowEnv string

// New returns the Python ecosystem adapter.
func New() ecosystem.Ecosystem { return driver{} }

type driver struct{}

// Spec describes the Python ecosystem.
func (driver) Spec() ecosystem.Spec {
	return ecosystem.Spec{
		ID:             "python",
		Language:       "Python",
		Manifests:      []string{"pyproject.toml"},
		PublishTargets: []string{"pypi"},
		WorkflowSetup:  workflowSetup,
		WorkflowEnv:    workflowEnv,
		Tools: []ecosystem.Tool{
			{Name: "uv", Command: "uv"},
			{Name: "gh", Command: "gh"},
		},
	}
}

// Publisher returns the PyPI publisher.
func (driver) Publisher() ecosystem.Publisher { return publisher{} }
