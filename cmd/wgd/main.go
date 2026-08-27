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
	return "wgctrl-rest " + version, buildTime, runtime.GOOS + "-" + runtime.GOARCH
}

func main() {
	var (
		listen   = flag.StringP("listen", "a", "0.0.0.0:6791", "REST API 监听地址")
		metadata = flag.StringP("metadata", "m", wgmeta.DefaultPath(), "节点名称元数据目录或具体 JSON 文件：目录将按 interface 生成 {name}.names.json，与 {name}.conf(.dpapi) 并列")
		hideKeys = flag.BoolP("hide-keys", "k", false, "查询响应中隐藏私钥与预共享密钥")
		apiKey   = flag.StringP("api-key", "x", "", "REST API 静态鉴权密钥（请求头 X-API-Key）；也可通过环境变量 WGD_API_KEY 设置。不设置则不启用鉴权⚠️")
	)

	network.SetAppVersion(appName, version, IsBeta, BuildTime)

	flag.Parse()

	// 命令行参数优先，其次读环境变量。
	if *apiKey == "" {
		*apiKey = os.Getenv("WGD_API_KEY")
	}
	if *apiKey == "" {
		logs.Warning("[WGD] ⚠️  未设置 REST API 鉴权密钥 (--api-key / -x 或环境变量 WGD_API_KEY)。任何人都可访问 WireGuard 配置与改接口，强烈建议在非本机部署时启用。")
	}

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

	if ds, derr := client.Devices(); derr != nil {
		logs.Warning("[WGD] 启动时枚举 WireGuard 接口失败: %v（请确认已安装并启动 WireGuard 内核驱动或用户态隧道管理器）", derr)
	} else if len(ds) == 0 {
		if runtime.GOOS == "windows" {
			logs.Warning("[WGD] 未检测到任何 WireGuard 接口。Windows 平台必须满足以下任一条件:\n  • 安装了官方 WireGuardNT 驱动，并已激活一条隧道（接口名如 WGTun/utun）\n  • 或正在运行基于命名管道 \\\\.\\pipe\\ProtectedPrefix\\Administrators\\WireGuard\\* 的用户态 WireGuard\n  当前 curl /api/v1/interfaces 返回 [] 属于正常，请先创建/激活接口。")
		} else {
			logs.Warning("[WGD] 未检测到任何 WireGuard 接口（/api/v1/interfaces 将返回 []）。请使用 wg-quick up / ip link add 等命令先创建 WireGuard 接口。")
		}
	} else {
		names := make([]string, 0, len(ds))
		for _, d := range ds { names = append(names, d.Name) }
		logs.Info("[WGD] 已检测到 %d 个 WireGuard 接口: [%s]", len(ds), strings.Join(names, ", "))
	}

	appVersion, buildTime, platform := runtimeBuildInfo()
	opts := []wgapi.Option{
		wgapi.Version(appVersion),
		wgapi.BuildInfo(buildTime, platform),
	}
	if *hideKeys {
		opts = append(opts, wgapi.HideKeys())
	}
	if *apiKey != "" {
		opts = append(opts, wgapi.APIKey(*apiKey))
	}
	api := wgapi.NewRestServer(client, *metadata, opts...)

	server := &http.Server{
		Addr:              *listen,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signalContext()
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		authStatus := "disabled"
		if *apiKey != "" { authStatus = "enabled (X-API-Key)" }
		logs.Info("监听 %s auth=%s", *listen, authStatus)
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
