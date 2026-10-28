package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func fingerprint(plan *Plan) (string, error) {
	payload := struct {
		Version  string        `json:"version"`
		Packages []PackagePlan `json:"packages"`
		Changes  []ChangePlan  `json:"changes"`
	}{Version: plan.Version.Next, Packages: plan.Packages, Changes: plan.Changes}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
