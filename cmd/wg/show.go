package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgcli"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgconf"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgmeta"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type showClient interface {
	Devices() ([]*wgtypes.Device, error)
	Device(string) (*wgtypes.Device, error)
	Close() error
}

var newShowClient = func() (showClient, error) { return wgctrl.New() }
var showNow = time.Now
var showMetadataPath = wgmeta.DefaultPath

func attachPeerNames(device *wgtypes.Device) error {
	return wgconf.AttachNames(device, showMetadataPath())
}

func show(args []string, _ io.Reader, out, errOut io.Writer) int {
	if len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		showUsage(errOut)
		return 0
	}
	if len(args) > 2 || (len(args) == 2 && args[0] == "interfaces") {
		showUsage(errOut)
		return 1
	}
	if len(args) == 2 && !validShowField(args[1]) {
		fmt.Fprintf(errOut, "无效的参数: `%s'\n", args[1])
		showUsage(errOut)
		return 1
	}

	client, err := newShowClient()
	if err != nil {
		fmt.Fprintf(errOut, "无法创建客户端: %v\n", err)
		return 1
	}
	defer client.Close()

	if len(args) > 0 && args[0] == "interfaces" {
		devices, err := client.Devices()
		if err != nil {
			fmt.Fprintf(errOut, "无法列出接口: %v\n", err)
			return 1
		}
		for i, device := range devices {
			if i > 0 {
				fmt.Fprint(out, " ")
			}
			fmt.Fprint(out, device.Name)
		}
		if len(devices) > 0 {
			fmt.Fprintln(out)
		}
		return 0
	}

	color := wgcli.ColorEnabled(out, os.Getenv("WG_COLOR_MODE"))
	if len(args) == 0 || args[0] == "all" {
		devices, err := client.Devices()
		if err != nil {
			fmt.Fprintf(errOut, "无法列出接口: %v\n", err)
			return 1
		}
		for i, device := range devices {
			if len(args) == 2 {
				if err := wgcli.Field(out, device, args[1], true); err != nil {
					fmt.Fprintln(errOut, err)
					return 1
				}
				continue
			}
			if err := attachPeerNames(device); err != nil {
				fmt.Fprintf(errOut, "警告: 无法读取节点名称: %v\n", err)
			}
			if i > 0 {
				fmt.Fprintln(out)
			}
			if err := wgcli.Pretty(out, device, showNow(), os.Getenv("WG_HIDE_KEYS") == "never", color); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
		}
		return 0
	}

	device, err := client.Device(args[0])
	if err != nil {
		fmt.Fprintf(errOut, "无法访问接口 %s: %v\n", args[0], err)
		return 1
	}
	if len(args) == 2 {
		if err := wgcli.Field(out, device, args[1], false); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return 0
	}
	if err := attachPeerNames(device); err != nil {
		fmt.Fprintf(errOut, "警告: 无法读取节点名称: %v\n", err)
	}
	if err := wgcli.Pretty(out, device, showNow(), os.Getenv("WG_HIDE_KEYS") == "never", color); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

func showUsage(w io.Writer) {
	fmt.Fprintln(w, "用法: wg show { <接口> | all | interfaces } [public-key | private-key | listen-port | fwmark | peers | preshared-keys | endpoints | allowed-ips | latest-handshakes | transfer | persistent-keepalive | dump]")
}

func validShowField(field string) bool {
	switch field {
	case "public-key", "private-key", "listen-port", "fwmark", "peers", "preshared-keys", "endpoints", "allowed-ips", "latest-handshakes", "transfer", "persistent-keepalive", "dump":
		return true
	default:
		return false
	}
}
