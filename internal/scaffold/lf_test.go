package scaffold

import "testing"

func TestNormalizeLF(t *testing.T) {
	got := string(normalizeLF([]byte("#!/bin/sh\r\nname: CI\r\n")))
	if got != "#!/bin/sh\nname: CI\n" {
		t.Fatalf("normalizeLF = %q", got)
	}
}
