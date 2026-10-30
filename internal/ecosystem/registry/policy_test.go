package registry_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// resolverLiterals are the ecosystem names core code must not branch on.
var resolverLiterals = map[string]bool{
	"npm": true, "python": true, "rust": true, "go": true,
	"pypi": true, "crates": true,
}

// policyAllowlist lists the files that still hold an ecosystem name while the
// corresponding refactor is in flight. Every entry needs a reason.
var policyAllowlist = map[string]string{
	// The Go asset pipeline moves behind ecosystem.AssetPlanner in the assets PR.
	"internal/release/plan.go":         "asset planning still branches on the Go resolver",
	"internal/release/assets/build.go": "the Go asset builder runs the go command",
}

// TestNoEcosystemNamesInCore guards the extension rule: adding a language must
// only touch internal/ecosystem. Core packages look the ecosystem up through
// the registry instead of naming it.
func TestNoEcosystemNamesInCore(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	var violations []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "doc_build", "site", ".quailegg":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "internal/ecosystem/") || rel == "internal/config/config.go" {
			return nil
		}
		if _, ok := policyAllowlist[rel]; ok {
			return nil
		}
		found, err := scanFile(path)
		if err != nil {
			return err
		}
		violations = append(violations, found...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("core code names an ecosystem; use the registry instead:\n%s", strings.Join(violations, "\n"))
	}
}

func scanFile(path string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.SelectorExpr:
			if ident, ok := value.X.(*ast.Ident); ok && ident.Name == "config" && strings.HasPrefix(value.Sel.Name, "Resolver") {
				out = append(out, fset.Position(value.Pos()).String())
			}
		case *ast.BasicLit:
			if value.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(value.Value)
			if err != nil {
				return true
			}
			if resolverLiterals[text] {
				out = append(out, fset.Position(value.Pos()).String())
			}
		}
		return true
	})
	return out, nil
}

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}
	return filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}
