package changes

import "testing"

func TestSplitConventional(t *testing.T) {
	cases := []struct {
		in          string
		typ         string
		scope       string
		description string
		ok          bool
	}{
		{in: "feat(cache): add ttl", typ: "feat", scope: "cache", description: "add ttl", ok: true},
		{in: "fix: avoid crash", typ: "fix", description: "avoid crash", ok: true},
		{in: "feat!: drop old api", typ: "feat", description: "drop old api", ok: true},
		{in: "not a conventional message", ok: false},
	}
	for _, tc := range cases {
		typ, scope, description, ok := SplitConventional(tc.in)
		if ok != tc.ok || typ != tc.typ || scope != tc.scope || description != tc.description {
			t.Errorf("SplitConventional(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
				tc.in, typ, scope, description, ok, tc.typ, tc.scope, tc.description, tc.ok)
		}
	}
}
