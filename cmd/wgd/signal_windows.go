//go:build windows

package main

import (
	"context"
	"os"
	"os/signal"
)

// signalContext 返回一个在收到 os.Interrupt（Ctrl+C）时取消的 context。
// Windows 不支持 SIGTERM。
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}
