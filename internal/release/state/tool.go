package state

// ToolInfo records the qegg build that produced a release state, so a state
// file written by a development build is distinguishable from a released one.
type ToolInfo struct {
	Version string `json:"version,omitempty"`
	Commit  string `json:"commit,omitempty"`
	Develop bool   `json:"develop,omitempty"`
}
