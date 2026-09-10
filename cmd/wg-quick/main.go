// Command wg-quick 是 wg-quick 的 Go 实现：解析 wg-quick 格式的配置，
// 完成 WireGuard 接口的创建 / 拆除与 Linux 网络集成（地址、路由、DNS、钩子）。
//
// 加密配置通过 wgctrl 库应用；接口生命周期与网络配置与上游 wg-quick 一致，
// 依赖 iproute2 / resolvconf。目前仅支持 Linux。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/internal/wgquick"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 1
	}
	switch args[0] {
	case "up":
		return quick(args[1:], wgquick.Up)
	case "down":
		return quick(args[1:], wgquick.Down)
	case "strip":
		return strip(args[1:])
	case "save":
		return save(args[1:])
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "wg-quick: 未知命令 %q\n", args[0])
		usage(os.Stderr)
		return 1
	}
}

func usage(w *os.File) {
	fmt.Fprintf(w, "用法: wg-quick [ up | down | strip | save ] <配置文件>\n\n")
	fmt.Fprintf(w, "  up    根据配置文件创建并配置 WireGuard 接口\n")
	fmt.Fprintf(w, "  down  拆除接口（地址与路由随接口自动清理）\n")
	fmt.Fprintf(w, "  strip 输出去除网络字段后的 wg(8) setconf 配置\n")
	fmt.Fprintf(w, "  save  把设备当前的加密配置合并写回配置文件\n\n")
	fmt.Fprintf(w, "接口名取自配置文件名（去掉 .conf 后缀），例如 wg0.conf -> wg0。\n")
}

// load 读取并解析配置文件，接口名取自文件名。
func load(path string) (*wgquick.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg, err := wgquick.Parse(f)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	cfg.Name = strings.TrimSuffix(base, ext)
	return cfg, nil
}

// quick 是 up / down 的公共入口。
func quick(args []string, fn func(*wgquick.Config) error) int {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "用法: wg-quick %s <配置文件>\n", os.Args[1])
		return 1
	}
	cfg, err := load(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := fn(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "wg-quick: %v\n", err)
		return 1
	}
	return 0
}

func strip(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "用法: wg-quick strip <配置文件>")
		return 1
	}
	cfg, err := load(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := wgquick.Strip(os.Stdout, cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func save(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "用法: wg-quick save <配置文件>")
		return 1
	}
	if err := wgquick.Save(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "wg-quick: %v\n", err)
		return 1
	}
	return 0
}
