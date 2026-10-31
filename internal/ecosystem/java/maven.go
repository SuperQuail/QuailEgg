package java

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// pomParent is the <parent> reference of a Maven module.
type pomParent struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
}

// pomProperties is the subset of <properties> qegg understands.
type pomProperties struct {
	Revision string `xml:"revision"`
}

// pom is the subset of pom.xml qegg reads.
type pom struct {
	GroupID      string        `xml:"groupId"`
	ArtifactID   string        `xml:"artifactId"`
	Version      string        `xml:"version"`
	Packaging    string        `xml:"packaging"`
	Parent       *pomParent    `xml:"parent"`
	Properties   pomProperties `xml:"properties"`
	Modules      []string      `xml:"modules>module"`
	Dependencies []struct {
		GroupID    string `xml:"groupId"`
		ArtifactID string `xml:"artifactId"`
	} `xml:"dependencies>dependency"`
	DistributionManagement *struct {
		Repository *struct {
			URL string `xml:"url"`
		} `xml:"repository"`
	} `xml:"distributionManagement"`
}

// readPom parses one pom.xml.
func readPom(file string) (*pom, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var doc pom
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return &doc, nil
}

// pomVersion resolves the project version. A module without a version of its
// own inherits the parent version; a ${revision} placeholder resolves through
// <properties><revision>.
func pomVersion(doc *pom) (string, bool) {
	raw := strings.TrimSpace(doc.Version)
	if raw != "" {
		if strings.HasPrefix(raw, "${") {
			name := strings.TrimSuffix(strings.TrimPrefix(raw, "${"), "}")
			if name == "revision" {
				return strings.TrimSpace(doc.Properties.Revision), false
			}
			return "", false
		}
		return raw, false
	}
	if doc.Parent != nil {
		return strings.TrimSpace(doc.Parent.Version), true
	}
	return "", false
}

// pomPublishes reports whether the project declares a Maven repository to
// deploy to. A project without one is an application artifact.
func pomPublishes(doc *pom) bool {
	return doc.DistributionManagement != nil && doc.DistributionManagement.Repository != nil &&
		strings.TrimSpace(doc.DistributionManagement.Repository.URL) != ""
}

// mavenPackage converts one pom.xml into an ecosystem package.
func mavenPackage(dir, manifest string, doc *pom) (ecosystem.Package, bool) {
	name := strings.TrimSpace(doc.ArtifactID)
	if name == "" {
		return ecosystem.Package{}, false
	}
	version, inherited := pomVersion(doc)
	deps := make([]string, 0, len(doc.Dependencies))
	for _, dep := range doc.Dependencies {
		if id := strings.TrimSpace(dep.ArtifactID); id != "" {
			deps = append(deps, id)
		}
	}
	return ecosystem.Package{
		Name:      name,
		Path:      dir,
		Resolver:  "java",
		Version:   version,
		Inherited: inherited,
		Publish:   pomPublishes(doc),
		Manifest:  manifest,
		Deps:      deps,
	}, true
}

// setMavenVersion rewrites the project version or its revision property.
func setMavenVersion(root string, pkg ecosystem.Package, next string) error {
	manifest, ok := packageManifest(root, pkg)
	if !ok {
		return errors.New(i18n.Tf("ecosystem.err.inspect_manifest", pkg.Name, fsx.Join(root, pkg.Path)))
	}
	file := fsx.Join(root, manifest)
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	doc, err := readPom(file)
	if err != nil {
		return err
	}
	raw := strings.TrimSpace(doc.Version)
	switch {
	case raw == "":
		if doc.Parent != nil {
			return nil
		}
		return errors.New(i18n.Tf("java.err.version_missing", file))
	case strings.HasPrefix(raw, "${"):
		if strings.TrimSuffix(strings.TrimPrefix(raw, "${"), "}") != "revision" {
			return errors.New(i18n.Tf("java.err.version_form", raw, file))
		}
		updated, ok := setElementText(string(data), []string{"project", "properties", "revision"}, next)
		if !ok {
			return errors.New(i18n.Tf("java.err.revision_missing", file))
		}
		return fsx.WriteFileAtomic(file, []byte(updated), 0o644)
	}
	updated, ok := setElementText(string(data), []string{"project", "version"}, next)
	if !ok {
		return errors.New(i18n.Tf("java.err.version_missing", file))
	}
	return fsx.WriteFileAtomic(file, []byte(updated), 0o644)
}

// xmlToken is one tag of an XML document.
type xmlToken struct {
	kind       int
	name       string
	start, end int
}

// Tag kinds of xmlToken.
const (
	tokenStart = iota
	tokenEnd
	tokenSelf
)

// tokenizeXML splits an XML document into tags, skipping comments, CDATA,
// processing instructions and declarations.
func tokenizeXML(text string) []xmlToken {
	var out []xmlToken
	i := 0
	for i < len(text) {
		rel := strings.IndexByte(text[i:], '<')
		if rel < 0 {
			break
		}
		start := i + rel
		rest := text[start:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			if k := strings.Index(rest, "-->"); k >= 0 {
				i = start + k + 3
				continue
			}
			return out
		case strings.HasPrefix(rest, "<![CDATA["):
			if k := strings.Index(rest, "]]>"); k >= 0 {
				i = start + k + 3
				continue
			}
			return out
		case strings.HasPrefix(rest, "<?"):
			if k := strings.Index(rest, "?>"); k >= 0 {
				i = start + k + 2
				continue
			}
			return out
		case strings.HasPrefix(rest, "<!"):
			if k := strings.IndexByte(rest, '>'); k >= 0 {
				i = start + k + 1
				continue
			}
			return out
		}
		end := tagEnd(text, start)
		if end < 0 {
			return out
		}
		name, kind := tagInfo(text[start:end])
		if name != "" {
			out = append(out, xmlToken{kind: kind, name: name, start: start, end: end})
		}
		i = end
	}
	return out
}

// tagEnd returns the index just after the '>' of the tag starting at start.
func tagEnd(text string, start int) int {
	var quote byte
	for i := start + 1; i < len(text); i++ {
		c := text[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '>':
			return i + 1
		}
	}
	return -1
}

// tagInfo returns the element name and kind of one raw tag.
func tagInfo(tag string) (string, int) {
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(tag, "<"), ">"))
	kind := tokenStart
	if strings.HasPrefix(body, "/") {
		kind = tokenEnd
		body = strings.TrimSpace(body[1:])
	} else if strings.HasSuffix(body, "/") {
		kind = tokenSelf
		body = strings.TrimSpace(strings.TrimSuffix(body, "/"))
	}
	name := body
	if i := strings.IndexAny(name, " \t\r\n"); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		name = name[i+1:]
	}
	return name, kind
}

// findElement returns the inner span of the element at path.
func findElement(text string, path ...string) (int, int, bool) {
	var stack []xmlToken
	for _, tok := range tokenizeXML(text) {
		switch tok.kind {
		case tokenStart:
			stack = append(stack, tok)
		case tokenEnd:
			if len(stack) == 0 {
				continue
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if open.name != tok.name || len(path) != len(stack)+1 {
				continue
			}
			match := true
			for i, ancestor := range stack {
				if ancestor.name != path[i] {
					match = false
					break
				}
			}
			if match && open.name == path[len(path)-1] {
				return open.end, tok.start, true
			}
		}
	}
	return 0, 0, false
}

// setElementText replaces the text content of the element at path.
func setElementText(text string, path []string, value string) (string, bool) {
	start, end, ok := findElement(text, path...)
	if !ok || start > end {
		return text, false
	}
	return text[:start] + value + text[end:], true
}
