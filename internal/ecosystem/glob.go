package ecosystem

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ExpandGlobs resolves workspace member patterns to existing directories.
func ExpandGlobs(root string, patterns []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if !strings.ContainsAny(pattern, "*?[{") {
			abs := filepath.Join(root, filepath.FromSlash(pattern))
			if info, err := os.Stat(abs); err == nil && info.IsDir() {
				rel := cleanRel(root, abs)
				if !seen[rel] {
					seen[rel] = true
					out = append(out, rel)
				}
			}
			continue
		}
		matches, err := doublestar.FilepathGlob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || !info.IsDir() {
				continue
			}
			rel := cleanRel(root, match)
			if seen[rel] {
				continue
			}
			seen[rel] = true
			out = append(out, rel)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Exclude filters rel patterns out of dirs.
func Exclude(dirs []string, patterns []string) []string {
	if len(patterns) == 0 {
		return dirs
	}
	var out []string
	for _, dir := range dirs {
		skip := false
		for _, pattern := range patterns {
			ok, err := doublestar.Match(pattern, dir)
			if err == nil && ok {
				skip = true
				break
			}
			ok, err = doublestar.Match(pattern, dir+"/")
			if err == nil && ok {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, dir)
		}
	}
	return out
}

func cleanRel(root, abs string) string {
	if rel, err := filepath.Rel(root, abs); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(abs)
}
