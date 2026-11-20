package java

import "testing"

// TestParseMavenRangeSeparatesLiteralsFromRanges pins the boundary between a
// version literal, a range that can be evaluated and one that cannot.
func TestParseMavenRangeSeparatesLiteralsFromRanges(t *testing.T) {
	cases := []struct {
		expr    string
		isRange bool
		wantErr bool
	}{
		{"1.2.3", false, false},
		{"1.2.3-SNAPSHOT", false, false},
		{"1.0.0.RELEASE", false, false},
		{"[1.0,2.0)", true, false},
		{"[1.0, 2.0)", true, false},
		{"[1.0,2.0]", true, false},
		{"(1.0,2.0)", true, false},
		{"[1.0,)", true, false},
		{"(,2.0]", true, false},
		{"[1.0]", true, false},
		{"[1.0,1.5),[2.0,2.5)", true, false},
		{"[1.0,1.5), (2.0,2.5]", true, false},
		{"[1.0,2.0", true, true},
		{"(1.0)", true, true},
		{"[1.0,2.0,3.0)", true, true},
		{"[,]", true, true},
		{"[1.0]extra", true, true},
		{"1.0,2.0", true, true},
	}
	for _, tc := range cases {
		ranges, isRange, err := parseMavenRange(tc.expr)
		if isRange != tc.isRange {
			t.Errorf("%q: isRange = %v, want %v", tc.expr, isRange, tc.isRange)
			continue
		}
		if (err != nil) != tc.wantErr {
			t.Errorf("%q: err = %v, want error %v", tc.expr, err, tc.wantErr)
			continue
		}
		if err == nil && isRange && len(ranges) == 0 {
			t.Errorf("%q: no interval parsed", tc.expr)
		}
	}
}

// TestRangeContains follows Maven's inclusive and exclusive bounds.
func TestRangeContains(t *testing.T) {
	cases := []struct {
		expr    string
		version string
		inside  bool
		wantErr bool
	}{
		{"[1.0,2.0)", "1.0", true, false},
		{"[1.0,2.0)", "1.5", true, false},
		{"[1.0,2.0)", "2.0", false, false},
		{"[1.0,2.0)", "0.9", false, false},
		{"[1.0,2.0]", "2.0", true, false},
		{"(1.0,2.0)", "1.0", false, false},
		{"(1.0,2.0)", "1.9.9", true, false},
		{"[1.0,)", "99.0.0", true, false},
		{"(,2.0]", "2.0", true, false},
		{"(,2.0]", "2.0.1", false, false},
		{"[1.0]", "1.0", true, false},
		{"[1.0]", "1.0.1", false, false},
		{"[1.0,1.5),[2.0,2.5)", "2.2.0", true, false},
		{"[1.0,1.5),[2.0,2.5)", "1.7.0", false, false},
		{"[1.0,2.0)", "1.2.3-SNAPSHOT", true, false},
		{"[foo,bar)", "1.0.0", false, true},
		{"[1.0,bar)", "1.0.0", false, true},
	}
	for _, tc := range cases {
		ranges, _, err := parseMavenRange(tc.expr)
		if err != nil {
			t.Errorf("%q: parse: %v", tc.expr, err)
			continue
		}
		inside, err := rangeContains(ranges, tc.version)
		if (err != nil) != tc.wantErr {
			t.Errorf("%q with %s: err = %v, want error %v", tc.expr, tc.version, err, tc.wantErr)
			continue
		}
		if err == nil && inside != tc.inside {
			t.Errorf("%q contains %s = %v, want %v", tc.expr, tc.version, inside, tc.inside)
		}
	}
}
