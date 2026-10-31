package release

import (
	"fmt"
	"github.com/SuperQuail/QuailEgg/internal/changelog"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
	"github.com/SuperQuail/QuailEgg/internal/release/state"
	"strings"
)

func (p *Plan) markerFile(d *Deps) *state.Marker {
	marker := &state.Marker{
		SchemaVersion: 1,
		Version:       p.Version.Next,
		CreatedAt:     d.Now(),
		Mode:          d.Cfg.Release.Mode,
		Channel:       p.Channel,
		Fingerprint:   p.Fingerprint,
		Verified: state.MarkerVerified{
			Commands: append([]string(nil), d.Cfg.Verify.Commands...),
			Skipped:  d.Options.SkipVerify,
			At:       d.Now(),
		},
		Qegg: d.toolInfo(),
	}
	if p.Repo.Valid() {
		marker.Repo = state.MarkerRepo{Host: p.Repo.Host, Owner: p.Repo.Owner, Name: p.Repo.Name, URL: p.Repo.URL()}
	}
	for _, pkg := range p.Packages {
		marker.Packages = append(marker.Packages, state.MarkerPackage{
			Name: pkg.Name, Path: pkg.Path, Resolver: pkg.Resolver,
			Current: pkg.Current, Next: pkg.Next, Bump: pkg.Bump, Tag: pkg.Tag, Publish: pkg.Publish,
		})
	}
	for _, release := range p.Releases {
		marker.Releases = append(marker.Releases, state.MarkerRelease{
			Name: release.Name, Version: release.Version, Tag: release.Tag,
			PrevTag: release.PrevTag, Packages: append([]string(nil), release.Packages...),
			Prerelease: release.Prerelease,
		})
	}
	for _, change := range p.Changes {
		marker.Changes = append(marker.Changes, state.MarkerChange{
			File: change.File, Type: change.Type, Scope: change.Scope,
			Bump: change.Bump, Packages: append([]string(nil), change.Packages...), Body: change.Body,
			Commit: change.Commit, CommitURL: change.CommitURL,
			PullNumber: change.PullNumber, PullURL: change.PullURL, Author: change.Author,
		})
	}
	// A prerelease marker also records which pending records the previous
	// prereleases already showed. The publish run reads the same marker and
	// needs the baseline to render only this release's notes.
	if p.Channel != "" && p.Marker != nil {
		pending := make(map[string]bool, len(p.Changes))
		for _, change := range p.Changes {
			pending[change.File] = true
		}
		seen := make(map[string]bool, len(p.Marker.Changes))
		for _, change := range p.Marker.Changes {
			if pending[change.File] && !seen[change.File] {
				marker.PreviousChanges = append(marker.PreviousChanges, change.File)
				seen[change.File] = true
			}
		}
	}
	return marker
}

func (p *Plan) pullRequestBody(d *Deps) string {
	var b strings.Builder
	b.WriteString(i18n.Tf("release.pr.header", p.Version.Next) + "\n\n")
	if p.Version.Next == "" {
		b.WriteString(i18n.Tf("release.pr.header_multi", len(p.Releases)) + "\n\n")
	}
	if p.Channel != "" {
		b.WriteString(i18n.Tf("release.pr.prerelease", p.Channel) + "\n\n")
	}
	b.WriteString(i18n.T("release.pr.version") + "\n\n")
	for _, pkg := range p.Packages {
		if pkg.Next == pkg.Current {
			continue
		}
		fmt.Fprintf(&b, i18n.T("release.pr.package")+"\n", pkg.Name, pkg.Current, pkg.Next, pkg.Bump)
	}
	changes := p.displayChanges()
	if len(changes) > 0 {
		b.WriteString("\n" + i18n.T("release.pr.changes") + "\n\n")
		for _, change := range changes {
			fmt.Fprintf(&b, i18n.T("release.pr.line")+"\n", change.Body)
		}
	}
	if len(d.Cfg.Verify.Commands) > 0 {
		b.WriteString("\n" + i18n.T("release.pr.verify") + "\n\n")
		for _, command := range d.Cfg.Verify.Commands {
			fmt.Fprintf(&b, i18n.T("release.pr.command")+"\n", command)
		}
	}
	b.WriteString("\n" + i18n.T("release.pr.changelog") + "\n\n")
	for _, section := range ChangelogSections(p, d.Now()) {
		b.WriteString(changelog.RenderBody(section))
		b.WriteString("\n")
	}
	return b.String()
}
