package scaffold

import "embed"

// assetFS holds every file qegg init scaffolds into a repository. The tree
// mirrors the generated layout, so a new output is added by dropping a file
// under assets/ and one line into the catalog in artifacts.go.
//
//go:embed assets
var assetFS embed.FS
