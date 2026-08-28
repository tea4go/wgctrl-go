package wgcli

import (
	"bytes"
	"testing"
)

func TestColorEnabled(t *testing.T) {
	for _, test := range []struct {
		name   string
		mode   string
		isTTY  bool
		wanted bool
	}{
		{"always pipe", "always", false, true},
		{"always tty", "always", true, true},
		{"never pipe", "never", false, false},
		{"never tty", "never", true, false},
		{"auto pipe", "", false, false},
		{"auto tty", "", true, true},
		{"other pipe", "auto", false, false},
		{"other tty", "auto", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := colorEnabled(test.mode, test.isTTY); got != test.wanted {
				t.Fatalf("colorEnabled(%q, %v) = %v, want %v", test.mode, test.isTTY, got, test.wanted)
			}
		})
	}
}

func TestColorEnabledForWriter(t *testing.T) {
	if ColorEnabled(&bytes.Buffer{}, "") {
		t.Fatal("buffer reported as terminal")
	}
	if !ColorEnabled(&bytes.Buffer{}, "always") {
		t.Fatal("always mode did not enable color")
	}
}
