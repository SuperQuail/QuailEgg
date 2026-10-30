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
	dir := fsx.Join(root, pkg.Path)
	var out []ecosystem.VersionFile
	for _, rel := range []string{"tauri.conf.json", "src-tauri/tauri.conf.json"} {
		if !fsx.Exists(filepath.Join(dir, filepath.FromSlash(rel))) {
			continue
		}
		out = append(out, ecosystem.VersionFile{Path: rel, Format: "json", Key: "version"})
	}
	return out
}

var _ ecosystem.VersionFileAdvisor = driver{}
