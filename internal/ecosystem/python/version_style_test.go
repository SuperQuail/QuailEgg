package python_test

import (
	"testing"

	"github.com/SuperQuail/QuailEgg/internal/ecosystem"
	"github.com/SuperQuail/QuailEgg/internal/ecosystem/python"
)

func versionStyle(t *testing.T) ecosystem.VersionStyle {
	t.Helper()
	style, ok := python.New().(ecosystem.VersionStyle)
	if !ok {
		t.Fatal("python adapter does not implement ecosystem.VersionStyle")
	}
	return style
}

func TestTagVersion(t *testing.T) {
	style := versionStyle(t)
	cases := map[string]string{
		"2.0.2":           "2.0.2",
		"2.0.2-a14":       "2.0.2a14",
		"2.0.2-alpha.14":  "2.0.2a14",
		"2.0.2a14":        "2.0.2a14",
		"2.0.2-beta.3":    "2.0.2b3",
		"2.0.2-rc.1":      "2.0.2rc1",
		"2.0.2-preview.2": "2.0.2rc2",
	}
	for in, want := range cases {
		if got := style.TagVersion(in); got != want {
			t.Errorf("TagVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitPrerelease(t *testing.T) {
	style := versionStyle(t)
	for _, version := range []string{"2.0.2-a14", "2.0.2a14", "2.0.2-alpha.14"} {
		base, channel, counter, ok := style.SplitPrerelease(version)
		if !ok || base != "2.0.2" || channel != "alpha" || counter != 14 {
			t.Errorf("SplitPrerelease(%q) = (%q, %q, %d, %v)", version, base, channel, counter, ok)
		}
	}
	if _, _, _, ok := style.SplitPrerelease("2.0.2"); ok {
		t.Error("a stable version must not split")
	}
	if _, _, _, ok := style.SplitPrerelease("2.0.2-foo.1"); ok {
		t.Error("an unknown channel must not split")
	}
}

func TestPrerelease(t *testing.T) {
	style := versionStyle(t)
	cases := []struct {
		channel string
		counter int
		want    string
	}{
		{"alpha", 15, "2.0.2-a15"},
		{"beta", 2, "2.0.2-b2"},
		{"rc", 1, "2.0.2-rc1"},
	}
	for _, tc := range cases {
		got, err := style.Prerelease("2.0.2", tc.channel, tc.counter)
		if err != nil || got != tc.want {
			t.Errorf("Prerelease(%q, %d) = (%q, %v), want %q", tc.channel, tc.counter, got, err, tc.want)
		}
	}
	if _, err := style.Prerelease("2.0.2-a14", "alpha", 1); err == nil {
		t.Error("Prerelease must reject a prerelease base")
	}
	if _, err := style.Prerelease("2.0.2", "alpha", 0); err == nil {
		t.Error("Prerelease must reject a zero counter")
	}
}
