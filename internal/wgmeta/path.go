package wgmeta

import (
	"os"
	"path/filepath"
	"runtime"
)

func DefaultPath() string {
	if value := os.Getenv("WGCTRL_PEER_METADATA_FILE"); value != "" {
		return value
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramData"), "wgctrl-go", "peer-names.json")
	case "darwin":
		return "/Library/Application Support/wgctrl-go/peer-names.json"
	default:
		return "/var/lib/wgctrl-go/peer-names.json"
	}
}
