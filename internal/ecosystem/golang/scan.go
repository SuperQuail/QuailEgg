package golang

import (
	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/i18n"
)

// ScanManifest reports nested Go modules as unsupported: qegg only manages the
// module at the repository root.
func (driver) ScanManifest(root string, pkg ecosystem.Package) (ecosystem.ManifestScan, error) {
	if pkg.Path != "." {
		return ecosystem.ManifestScan{
			Package: ecosystem.Package{Path: pkg.Path, Manifest: pkg.Manifest, Resolver: pkg.Resolver},
			Reason:  ecosystem.ScanReasonNestedGo,
			Detail:  i18n.T("scan.detail.nested_go"),
		}, nil
	}
	packages, err := (driver{}).Detect(root)
	if err != nil {
		return ecosystem.ManifestScan{}, err
	}
	if len(packages) == 0 {
		return ecosystem.ManifestScan{}, nil
	}
	return ecosystem.ManifestScan{Package: packages[0], OK: true}, nil
}

var _ ecosystem.ManifestScanner = driver{}
