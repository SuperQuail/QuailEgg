package java

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

// pomParent is the <parent> reference of a Maven module.
type pomParent struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
}

// pomProperties is the subset of <properties> qegg understands. Maven only
// allows these three placeholders in a version.
type pomProperties struct {
	Revision   string `xml:"revision"`
	SHA1       string `xml:"sha1"`
	Changelist string `xml:"changelist"`
}

// pomDependency is one <dependency> entry.
type pomDependency struct {
	GroupID    string `xml:"groupId"`
	ArtifactID string `xml:"artifactId"`
	Version    string `xml:"version"`
	Scope      string `xml:"scope"`
}

// pomRepository is one deployable repository.
type pomRepository struct {
	ID  string `xml:"id"`
	URL string `xml:"url"`
}

// pomDistributionManagement is the <distributionManagement> section.
type pomDistributionManagement struct {
	Repository *pomRepository `xml:"repository"`
}

// pom is the subset of pom.xml qegg reads.
type pom struct {
	GroupID                string                     `xml:"groupId"`
	ArtifactID             string                     `xml:"artifactId"`
	Version                string                     `xml:"version"`
	Packaging              string                     `xml:"packaging"`
	Parent                 *pomParent                 `xml:"parent"`
	Properties             pomProperties              `xml:"properties"`
	Modules                []string                   `xml:"modules>module"`
	Dependencies           []pomDependency            `xml:"dependencies>dependency"`
	ManagedDependencies    []pomDependency            `xml:"dependencyManagement>dependencies>dependency"`
	DistributionManagement *pomDistributionManagement `xml:"distributionManagement"`
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

// xmlElement is one element range in a document.
type xmlElement struct {
	name                                       string
	outerStart, innerStart, innerEnd, outerEnd int
}

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

// walkElements visits every element with its ancestor path.
func walkElements(text string, visit func(path []string, el xmlElement)) {
	type open struct {
		name         string
		start, inner int
	}
	var stack []open
	for _, tok := range tokenizeXML(text) {
		switch tok.kind {
		case tokenStart:
			stack = append(stack, open{name: tok.name, start: tok.start, inner: tok.end})
		case tokenEnd:
			if len(stack) == 0 {
				continue
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if top.name != tok.name {
				continue
			}
			path := make([]string, 0, len(stack)+1)
			for _, item := range stack {
				path = append(path, item.name)
			}
			path = append(path, top.name)
			visit(path, xmlElement{name: top.name, outerStart: top.start, innerStart: top.inner, innerEnd: tok.start, outerEnd: tok.end})
		case tokenSelf:
			path := make([]string, 0, len(stack)+1)
			for _, item := range stack {
				path = append(path, item.name)
			}
			path = append(path, tok.name)
			visit(path, xmlElement{name: tok.name, outerStart: tok.start, innerStart: tok.end, innerEnd: tok.end, outerEnd: tok.end})
		}
	}
}

// findElements returns every element at the exact path.
func findElements(text string, path ...string) []xmlElement {
	var out []xmlElement
	walkElements(text, func(candidate []string, el xmlElement) {
		if samePath(candidate, path) {
			out = append(out, el)
		}
	})
	return out
}

// findElement returns the first element at path.
func findElement(text string, path ...string) (xmlElement, bool) {
	all := findElements(text, path...)
	if len(all) == 0 {
		return xmlElement{}, false
	}
	return all[0], true
}

// directChildren returns the direct child elements called name.
func directChildren(text string, parent xmlElement, name string) []xmlElement {
	start := parent.innerStart
	if start > len(text) {
		return nil
	}
	end := parent.innerEnd
	if end > len(text) {
		end = len(text)
	}
	sub := text[start:end]
	var out []xmlElement
	walkElements(sub, func(path []string, el xmlElement) {
		if len(path) != 1 || path[0] != name {
			return
		}
		el.outerStart += start
		el.outerEnd += start
		el.innerStart += start
		el.innerEnd += start
		out = append(out, el)
	})
	return out
}

// elementText returns the trimmed text content of one element.
func elementText(text string, el xmlElement) string {
	if el.innerStart > len(text) || el.innerEnd > len(text) || el.innerStart > el.innerEnd {
		return ""
	}
	return strings.TrimSpace(text[el.innerStart:el.innerEnd])
}

// setElementText replaces the text content of an element.
func setElementText(text string, el xmlElement, value string) string {
	if el.innerStart > len(text) || el.innerEnd > len(text) || el.innerStart > el.innerEnd {
		return text
	}
	return text[:el.innerStart] + value + text[el.innerEnd:]
}

// samePath reports whether a and b contain the same element names.
func samePath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
