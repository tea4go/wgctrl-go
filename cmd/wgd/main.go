// Command wgd 是 wgctrl-go 的守护进程：以 REST API 的形式常驻运行，
// 通过 HTTP 请求完成 wg(8) 全部子命令对应的 WireGuard 设备管理操作。
//
// 用法:
//
//	wgd [-listen 127.0.0.1:8080] [-metadata 路径] [-hide-keys]
//
// 端点列表见 internal/wgapi 包的文档。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgapi"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgmeta"
)

const version = "wgctrl-go wgd v1.0.20260223"

func main() {
	var (
		listen   = flag.String("listen", "127.0.0.1:8080", "REST API 监听地址")
		metadata = flag.String("metadata", wgmeta.DefaultPath(), "节点名称元数据文件路径")
		hideKeys = flag.Bool("hide-keys", false, "查询响应中隐藏私钥与预共享密钥")
	)
	flag.Parse()

	logger := log.New(os.Stderr, "wgd: ", log.LstdFlags)

	client, err := wgctrl.New()
	if err != nil {
		logger.Fatalf("无法创建 WireGuard 客户端: %v", err)
	}
	defer client.Close()

	opts := []wgapi.Option{
		wgapi.Version(version),
		wgapi.Logger(logger),
	}
	if *hideKeys {
		opts = append(opts, wgapi.HideKeys())
	}
	api := wgapi.New(client, *metadata, opts...)

	server := &http.Server{
		Addr:              *listen,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signalContext()
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("监听 %s", *listen)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		logger.Fatalf("HTTP 服务异常退出: %v", err)
	case <-ctx.Done():
		logger.Printf("收到退出信号，正在关闭…")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "wgd: 关闭 HTTP 服务失败: %v\n", err)
	}
}
