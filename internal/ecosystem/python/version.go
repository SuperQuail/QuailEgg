package python

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// dynamicVersion resolves a dynamic version from hatch/setuptools configuration.
func dynamicVersion(pyprojectPath string) (string, error) {
	data, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return "", err
	}
	var doc pyProject
	if err := toml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	dir := filepath.Dir(pyprojectPath)
	targets := dynamicTargets(&doc)
	if len(targets) == 0 {
		return "", errors.New("dynamic version has no hatch/setuptools target")
	}
	for _, target := range targets {
		candidate := filepath.Join(dir, filepath.FromSlash(target))
		if value, err := readVersionAssignment(candidate); err == nil && value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("cannot read dynamic version from %s", strings.Join(targets, ", "))
}

func dynamicTargets(doc *pyProject) []string {
	var targets []string
	if doc.Tool != nil && doc.Tool.Hatch != nil && doc.Tool.Hatch.Version != nil && doc.Tool.Hatch.Version.Path != "" {
		targets = append(targets, doc.Tool.Hatch.Version.Path)
	}
	if doc.Tool != nil && doc.Tool.Setuptools != nil && doc.Tool.Setuptools.Dynamic != nil && doc.Tool.Setuptools.Dynamic.Version != nil {
		if file := doc.Tool.Setuptools.Dynamic.Version.File; file != "" {
			for _, part := range strings.Split(file, ",") {
				part = strings.TrimSpace(part)
				if part != "" {
					targets = append(targets, part)
				}
			}
		}
		if attr := doc.Tool.Setuptools.Dynamic.Version.Attr; attr != "" {
			targets = append(targets, attrCandidates(attr)...)
		}
	}
	return targets
}

// attrCandidates resolves a setuptools attribute into likely file paths.
func attrCandidates(attr string) []string {
	attr = strings.TrimSpace(attr)
	var module string
	if i := strings.Index(attr, ":"); i >= 0 {
		module = attr[:i]
	} else if i := strings.LastIndex(attr, "."); i >= 0 {
		module = attr[:i]
	} else {
		module = attr
	}
	module = strings.TrimSpace(module)
	if module == "" {
		return nil
	}
	rel := strings.ReplaceAll(module, ".", "/")
	return []string{
		rel + ".py",
		rel + "/__init__.py",
		"src/" + rel + ".py",
		"src/" + rel + "/__init__.py",
	}
}

var versionAssignment = regexp.MustCompile(`(?m)^\s*(?:__version__|version)\s*=\s*["']([^"']+)["']`)

func readVersionAssignment(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	matches := versionAssignment.FindAllStringSubmatch(string(data), -1)
	if len(matches) != 1 {
		return "", fmt.Errorf("%s: expected one version assignment, found %d", path, len(matches))
	}
	return matches[0][1], nil
}

func (driver) SetVersion(root string, pkg ecosystem.Package, next string) error {
	if _, _, _, _, ok := parsePythonVersion(next); !ok {
		return fmt.Errorf("version %q is not X.Y.Z or a PEP 440 prerelease", next)
	}
	pyprojectPath := filepath.Join(fsx.Join(root, pkg.Path), "pyproject.toml")
	data, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return err
	}
	var doc pyProject
	if err := toml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if doc.Project != nil && doc.Project.Version != "" {
		return replaceTableVersion(pyprojectPath, "project", next)
	}
	if doc.Tool != nil && doc.Tool.Poetry != nil && doc.Tool.Poetry.Version != "" {
		return replaceTableVersion(pyprojectPath, "tool.poetry", next)
	}
	if doc.Project != nil && containsString(doc.Project.Dynamic, "version") {
		target, err := dynamicTarget(pyprojectPath, &doc)
		if err != nil {
			return err
		}
		return replaceAssignment(target, next)
	}
	return fmt.Errorf("%s: no writable version field", pyprojectPath)
}

func dynamicTarget(pyprojectPath string, doc *pyProject) (string, error) {
	dir := filepath.Dir(pyprojectPath)
	for _, candidate := range dynamicTargets(doc) {
		path := filepath.Join(dir, filepath.FromSlash(candidate))
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s: cannot locate the dynamic version file", pyprojectPath)
}

func replaceTableVersion(path, table, next string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	inTable := false
	replaced := 0
	versionLine := regexp.MustCompile(`^(\s*)version\s*=\s*["'][^"']*["']\s*$`)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inTable = trimmed == "["+table+"]"
			continue
		}
		if !inTable {
			continue
		}
		if versionLine.MatchString(line) {
			indent := versionLine.FindStringSubmatch(line)[1]
			lines[i] = indent + "version = " + strconv.Quote(next)
			replaced++
		}
	}
	if replaced != 1 {
		return fmt.Errorf("%s: expected one version line in [%s], replaced %d", path, table, replaced)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

func replaceAssignment(path, next string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	matches := versionAssignment.FindAllStringSubmatchIndex(text, -1)
	if len(matches) != 1 {
		return fmt.Errorf("%s: expected one version assignment, found %d", path, len(matches))
	}
	m := matches[0]
	if len(m) < 4 || m[2] < 0 || m[3] < 0 {
		return fmt.Errorf("%s: version value was not captured", path)
	}
	updated := text[:m[2]] + next + text[m[3]:]
	return os.WriteFile(path, []byte(updated), 0o644)
}
