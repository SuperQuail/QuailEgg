package rust

import "github.com/SuperQuail/QuailEgg/internal/ecosystem"

// DefaultVerifyCommands runs the workspace test suite.
func (driver) DefaultVerifyCommands(string) []string {
	return []string{"cargo test"}
}

var _ ecosystem.InitAdvisor = driver{}
