package semver

import "testing"

func TestNext(t *testing.T) {
	cases := []struct {
		current string
		bump    Bump
		want    string
	}{
		{"1.2.3", Patch, "1.2.4"},
		{"1.2.3", Minor, "1.3.0"},
		{"1.2.3", Major, "2.0.0"},
	}
	for _, tc := range cases {
		got, err := Next(tc.current, tc.bump)
		if err != nil {
			t.Fatalf("Next(%q, %q): %v", tc.current, tc.bump, err)
		}
		if got.String() != tc.want {
			t.Errorf("Next(%q, %q) = %q, want %q", tc.current, tc.bump, got.String(), tc.want)
		}
	}
	if _, err := Next("not-a-version", Patch); err == nil {
		t.Error("expected an error for invalid semver")
	}
}

func TestParseBump(t *testing.T) {
	for _, value := range []string{"patch", "minor", "major"} {
		if _, err := ParseBump(value); err != nil {
			t.Errorf("ParseBump(%q): %v", value, err)
		}
	}
	if _, err := ParseBump("breaking"); err == nil {
		t.Error("expected an error for an unknown bump")
	}
}

func TestMax(t *testing.T) {
	if got := Max(Patch, Minor); got != Minor {
		t.Errorf("Max(patch, minor) = %q", got)
	}
	if got := Max(Major, Minor); got != Major {
		t.Errorf("Max(major, minor) = %q", got)
	}
}

func TestTag(t *testing.T) {
	if got := Tag("v", "1.2.3"); got != "v1.2.3" {
		t.Errorf("Tag(v, 1.2.3) = %q", got)
	}
}
