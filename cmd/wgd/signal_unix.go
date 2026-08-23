//go:build !windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// signalContext 返回一个在收到 SIGINT 或 SIGTERM 时取消的 context。
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
