package java

import (
	"errors"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// mavenSync keeps internal Maven references consistent after the manifests
// were bumped. It rewrites <parent><version> and internal dependency pins of
// managed packages; external coordinates are never touched.
func mavenSync(root string, pkgs []ecosystem.Package) ([]string, error) {
	if len(pkgs) == 0 {
		return nil, nil
	}
	var changed []string
	err := forEachReactor(root, pkgs, func(reactor *mavenReactor, group []ecosystem.Package) error {
		paths, err := mavenSyncReactor(root, reactor, group)
		if err != nil {
			return err
		}
		changed = append(changed, paths...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return uniqueSorted(changed), nil
}

// mavenSyncReactor syncs one loaded reactor.
func mavenSyncReactor(root string, reactor *mavenReactor, pkgs []ecosystem.Package) ([]string, error) {
	plan, err := mavenRoundPlan(root, reactor, pkgs, nil)
	if err != nil {
		return nil, err
	}
	// The release executor checks the whole round before it writes anything, so
	// reaching a finding here means a caller skipped that check. Refusing the
	// write keeps the same promise: no manifest loses content the round cannot
	// preserve, and no version range is replaced by a version it excludes.
	if findings := plan.findings(); len(findings) > 0 {
		return nil, errors.New(mavenFindingList(findings))
	}
	var changed []string
	for _, planned := range plan.ordered() {
		if len(planned.writes) == 0 {
			continue
		}
		next := applyReplacements(planned.text, planned.writes)
		if err := fsx.WriteFileAtomic(fsx.Join(root, planned.manifest), []byte(next), 0o644); err != nil {
			return nil, err
		}
		changed = append(changed, planned.manifest)
	}
	return uniqueSorted(changed), nil
}

// mavenFindingList renders the findings of a round for a caller that skipped
// the preflight. The detailed, per-location message lives in the registry,
// which every release run goes through.
func mavenFindingList(findings []ecosystem.PreflightFinding) string {
	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		switch finding.Reason {
		case ecosystem.PreflightReasonRangeExcluded, ecosystem.PreflightReasonRangeUnparsable:
			lines = append(lines, i18n.Tf("java.err.range_pin", finding.Manifest, finding.Coordinates, finding.Range, finding.Target))
		case ecosystem.PreflightReasonEmpty:
			lines = append(lines, i18n.Tf("java.err.element_empty", finding.Manifest, finding.Element))
		default:
			lines = append(lines, i18n.Tf("java.err.element_unsafe", finding.Manifest, finding.Element))
		}
	}
	return strings.Join(lines, "\n")
}

// firstChildText returns the text of the first direct child called name.
func firstChildText(text string, parent xmlElement, name string) string {
	children := directChildren(text, parent, name)
	if len(children) == 0 {
		return ""
	}
	return elementText(text, children[0])
}

// textReplacement is one content replacement.
type textReplacement struct {
	start, end int
	value      string
}

// applyReplacements applies non-overlapping replacements from the end.
func applyReplacements(text string, replacements []textReplacement) string {
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	for _, replacement := range replacements {
		if replacement.start < 0 || replacement.end > len(text) || replacement.start > replacement.end {
			continue
		}
		text = text[:replacement.start] + replacement.value + text[replacement.end:]
	}
	return text
}

// uniqueSorted returns sorted unique paths.
func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = path.Clean(filepath.ToSlash(value))
		if value == "." || value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
