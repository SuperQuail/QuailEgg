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
		Fingerprint:   p.Fingerprint,
		Verified: state.MarkerVerified{
			Commands: append([]string(nil), d.Cfg.Verify.Commands...),
			Skipped:  d.Options.SkipVerify,
			At:       d.Now(),
		},
	}
	if p.Repo.Valid() {
		marker.Repo = state.MarkerRepo{Host: p.Repo.Host, Owner: p.Repo.Owner, Name: p.Repo.Name, URL: p.Repo.URL()}
	}
	for _, pkg := range p.Packages {
		marker.Packages = append(marker.Packages, state.MarkerPackage{
			Name: pkg.Name, Path: pkg.Path, Resolver: pkg.Resolver,
			Current: pkg.Current, Next: pkg.Next, Bump: pkg.Bump, Publish: pkg.Publish,
		})
	}
	for _, change := range p.Changes {
		marker.Changes = append(marker.Changes, state.MarkerChange{
			File: change.File, Type: change.Type, Scope: change.Scope,
			Bump: change.Bump, Body: change.Body, Commit: change.Commit, CommitURL: change.CommitURL,
		})
	}
	return marker
}

func (p *Plan) pullRequestBody(d *Deps) string {
	var b strings.Builder
	b.WriteString(i18n.Tf("release.pr.header", p.Version.Next) + "\n\n")
	b.WriteString(i18n.T("release.pr.version") + "\n\n")
	for _, pkg := range p.Packages {
		fmt.Fprintf(&b, i18n.T("release.pr.package")+"\n", pkg.Name, pkg.Current, pkg.Next, pkg.Bump)
	}
	if len(p.Changes) > 0 {
		b.WriteString("\n" + i18n.T("release.pr.changes") + "\n\n")
		for _, change := range p.Changes {
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
	b.WriteString(changelog.RenderBody(ChangelogSection(p, d.Now())))
	return b.String()
}
