//go:build !linux

package wgquick

import (
	"fmt"
	"runtime"
)

// Up 在非 Linux 平台不受支持。
func Up(c *Config) error {
	return fmt.Errorf("wgquick: Up is not supported on %s", runtime.GOOS)
}

// Down 在非 Linux 平台不受支持。
func Down(c *Config) error {
	return fmt.Errorf("wgquick: Down is not supported on %s", runtime.GOOS)
}
