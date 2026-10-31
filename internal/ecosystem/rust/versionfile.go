package rust

import (
	"path/filepath"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// VersionFileCandidates lists files outside Cargo.toml that may carry the app
// version. Tauri keeps it in tauri.conf.json, either next to the crate or in a
// src-tauri directory of the workspace root.
func (driver) VersionFileCandidates(root string, pkg ecosystem.Package) []ecosystem.VersionFile {
	var out []ecosystem.VersionFile
	for _, rel := range tauriConfigs(root, pkg) {
		out = append(out, ecosystem.VersionFile{Path: rel, Format: "json", Key: "version"})
	}
	return out
}

// tauriConfigs lists the Tauri configuration files next to the crate.
func tauriConfigs(root string, pkg ecosystem.Package) []string {
	dir := fsx.Join(root, pkg.Path)
	var out []string
	for _, rel := range []string{"tauri.conf.json", "src-tauri/tauri.conf.json"} {
		if fsx.Exists(filepath.Join(dir, filepath.FromSlash(rel))) {
			out = append(out, rel)
		}
	}
	return out
}

// DefaultPublish keeps a Tauri application crate out of crates.io.
func (driver) DefaultPublish(root string, pkg ecosystem.Package) bool {
	if len(tauriConfigs(root, pkg)) == 0 {
		return pkg.Publish
	}
	return false
}

var (
	_ ecosystem.VersionFileAdvisor = driver{}
	_ ecosystem.PublishAdvisor     = driver{}
)
