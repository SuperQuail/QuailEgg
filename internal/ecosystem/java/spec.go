// Package java adapts JVM projects built with Maven (pom.xml) or Gradle
// (build.gradle, build.gradle.kts).
package java

import (
	_ "embed"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
)

//go:embed workflow-setup.yml
var workflowSetup string

//go:embed workflow-env.yml
var workflowEnv string

// New returns the Java ecosystem adapter.
func New() ecosystem.Ecosystem { return driver{} }

type driver struct{}

// Spec describes the Java ecosystem.
func (driver) Spec() ecosystem.Spec {
	return ecosystem.Spec{
		ID:             "java",
		Language:       "Java",
		Manifests:      []string{"pom.xml", "build.gradle", "build.gradle.kts"},
		PublishTargets: []string{"maven"},
		WorkflowSetup:  workflowSetup,
		WorkflowEnv:    workflowEnv,
		Tools: []ecosystem.Tool{
			{Name: "java", Command: "java"},
			{Name: "gh", Command: "gh"},
		},
	}
}

// Publisher returns the Maven repository publisher.
func (driver) Publisher() ecosystem.Publisher { return publisher{} }
