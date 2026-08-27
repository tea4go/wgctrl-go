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
		return filepath.Join(os.Getenv("ProgramData"), "WireGuard", "Data", "Configurations", "peer-names.json")
	case "darwin":
		return "/Library/Application Support/WireGuard/Configurations/peer-names.json"
	default:
		return "/etc/wireguard/peer-names.json"
	}
}
