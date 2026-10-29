package golang

import "testing"

func TestEndpointEscapesUppercase(t *testing.T) {
	got := endpoint("github.com/SuperQuail/QuailEgg", "v1.2.3")
	want := "https://proxy.golang.org/github.com/!super!quail/!quail!egg/@v/v1.2.3.info"
	if got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
}

func TestEndpointEscapesVersion(t *testing.T) {
	got := endpoint("example.com/demo", "v1.0.0-RC1")
	want := "https://proxy.golang.org/example.com/demo/@v/v1.0.0-!r!c1.info"
	if got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
}

func TestEndpointPrefixesBareVersion(t *testing.T) {
	tests := []struct {
		version string
		want    string
	}{
		{version: "0.4.0", want: "https://proxy.golang.org/example.com/demo/@v/v0.4.0.info"},
		{version: "0.4.0-alpha.1", want: "https://proxy.golang.org/example.com/demo/@v/v0.4.0-alpha.1.info"},
		{version: "0.1.0-RC1", want: "https://proxy.golang.org/example.com/demo/@v/v0.1.0-!r!c1.info"},
		{version: "v1.2.3", want: "https://proxy.golang.org/example.com/demo/@v/v1.2.3.info"},
	}
	for _, tc := range tests {
		if got := endpoint("example.com/demo", tc.version); got != tc.want {
			t.Errorf("endpoint(%q) = %q, want %q", tc.version, got, tc.want)
		}
	}
}

func TestEscapePathKeepsLowercase(t *testing.T) {
	if got := escapePath("example.com/demo/v2"); got != "example.com/demo/v2" {
		t.Fatalf("escapePath = %q", got)
	}
}
