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

func TestSplitPrerelease(t *testing.T) {
	tests := []struct {
		version string
		base    string
		channel string
		counter int
		ok      bool
	}{
		{version: "0.5.0-alpha.2", base: "0.5.0", channel: "alpha", counter: 2, ok: true},
		{version: "1.0.0-beta.11", base: "1.0.0", channel: "beta", counter: 11, ok: true},
		{version: "2.0.0-rc.1", base: "2.0.0", channel: "rc", counter: 1, ok: true},
		{version: "0.5.0", ok: false},
		{version: "0.5.0-alpha", ok: false},
		{version: "0.5.0-alpha.0", ok: false},
		{version: "0.5.0-next.1", ok: false},
		{version: "not-a-version", ok: false},
	}
	for _, tc := range tests {
		base, channel, counter, ok := SplitPrerelease(tc.version)
		if ok != tc.ok {
			t.Errorf("SplitPrerelease(%q) ok = %v, want %v", tc.version, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if base != tc.base || channel != tc.channel || counter != tc.counter {
			t.Errorf("SplitPrerelease(%q) = (%q, %q, %d), want (%q, %q, %d)",
				tc.version, base, channel, counter, tc.base, tc.channel, tc.counter)
		}
	}
}

func TestPrerelease(t *testing.T) {
	got, err := Prerelease("0.5.0", "alpha", 3)
	if err != nil {
		t.Fatalf("Prerelease: %v", err)
	}
	if want := "0.5.0-alpha.3"; got != want {
		t.Errorf("Prerelease = %q, want %q", got, want)
	}
	if _, err := Prerelease("0.5.0", "nightly", 1); err == nil {
		t.Error("expected an error for an unknown channel")
	}
	if _, err := Prerelease("0.5.0", "alpha", 0); err == nil {
		t.Error("expected an error for a zero counter")
	}
	if _, err := Prerelease("0.5.0-beta.1", "alpha", 1); err == nil {
		t.Error("expected an error for a prerelease base")
	}
}

func TestStableAndIsPrerelease(t *testing.T) {
	if got, had := Stable("1.2.3"); had || got != "1.2.3" {
		t.Errorf("Stable(1.2.3) = (%q, %v)", got, had)
	}
	if got, had := Stable("1.2.3-rc.4"); !had || got != "1.2.3" {
		t.Errorf("Stable(1.2.3-rc.4) = (%q, %v), want (1.2.3, true)", got, had)
	}
	if !IsPrerelease("1.2.3-rc.4") || IsPrerelease("1.2.3") {
		t.Error("IsPrerelease misclassified a version")
	}
}
