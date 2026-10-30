package golang

import "github.com/SuperQuail/QuailEgg/internal/ecosystem"

// DefaultVerifyCommands runs the module tests.
func (driver) DefaultVerifyCommands(string) []string {
	return []string{"go test ./..."}
}

var _ ecosystem.InitAdvisor = driver{}
