package wgcli

import (
	"io"

	"golang.org/x/term"
)

func ColorEnabled(w io.Writer, mode string) bool {
	if colorEnabled(mode, false) {
		return true
	}
	if mode == "never" {
		return false
	}
	fder, ok := w.(interface{ Fd() uintptr })
	return ok && colorEnabled(mode, term.IsTerminal(int(fder.Fd())))
}

func colorEnabled(mode string, isTTY bool) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	default:
		return isTTY
	}
}
