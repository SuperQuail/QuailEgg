package java

import (
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/platform/fsx"
)

// PreflightVersions inspects every Maven location the round would rewrite
// before the first write: the project versions, the parent versions, the
// internal dependency pins and the pins under dependencyManagement. Gradle
// packages keep their version in a script rather than in an element span, so
// there is no inner content to protect and they are left out.
//
// The packages are grouped the way the writes resolve them: the repository root
// reactor when it contains the package, otherwise the reactor of the package
// directory. A configured package outside the root reactor is written by
// SetVersion but never reached by the reference sync, so only its project
// version is checked.
func (driver) PreflightVersions(root string, round ecosystem.VersionRound) ([]ecosystem.PreflightFinding, error) {
	mavenPkgs := mavenPackages(root, round.Packages)
	if len(mavenPkgs) == 0 {
		return nil, nil
	}
	ownPkgs := mavenPackages(root, round.Own)
	if !fsx.Exists(fsx.Join(root, "pom.xml")) {
		return mavenPreflightWithoutRoot(root, mavenPkgs, ownPkgs)
	}
	reactor, err := loadMavenReactor(root)
	if err != nil {
		return nil, err
	}
	var findings []ecosystem.PreflightFinding
	group := make([]ecosystem.Package, 0, len(mavenPkgs))
	for _, pkg := range mavenPkgs {
		if reactor.nodeForDir(pkg.Path) == nil {
			continue
		}
		group = append(group, pkg)
	}
	if len(group) > 0 {
		planned, err := mavenRoundPlan(root, reactor, group, ownInReactor(reactor, ownPkgs))
		if err != nil {
			return nil, err
		}
		findings = append(findings, planned.findings()...)
	}
	for _, pkg := range mavenPkgs {
		if reactor.nodeForDir(pkg.Path) != nil {
			continue
		}
		outside, err := loadMavenReactorAt(root, pkg.Path)
		if err != nil {
			return nil, err
		}
		planned, err := mavenOwnPlan(root, outside, []ecosystem.Package{pkg})
		if err != nil {
			return nil, err
		}
		findings = append(findings, planned.findings()...)
	}
	return findings, nil
}

// mavenPreflightWithoutRoot checks a repository without a root pom, where every
// package is loaded with the reactor of its own directory.
func mavenPreflightWithoutRoot(root string, mavenPkgs, ownPkgs []ecosystem.Package) ([]ecosystem.PreflightFinding, error) {
	var findings []ecosystem.PreflightFinding
	for _, pkg := range mavenPkgs {
		reactor, err := loadMavenReactorAt(root, pkg.Path)
		if err != nil {
			return nil, err
		}
		planned, err := mavenRoundPlan(root, reactor, []ecosystem.Package{pkg}, ownInReactor(reactor, ownPkgs))
		if err != nil {
			return nil, err
		}
		findings = append(findings, planned.findings()...)
	}
	return findings, nil
}

// mavenPackages keeps the Maven packages of one round: a Gradle manifest is not
// part of this check.
func mavenPackages(root string, pkgs []ecosystem.Package) []ecosystem.Package {
	out := make([]ecosystem.Package, 0, len(pkgs))
	for _, pkg := range pkgs {
		manifest, ok := packageManifest(root, pkg)
		if !ok || kindOf(manifest) != buildMaven {
			continue
		}
		out = append(out, pkg)
	}
	return out
}

var _ ecosystem.VersionPreflight = driver{}
