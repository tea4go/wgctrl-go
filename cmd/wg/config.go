package main

import (
	"fmt"
	"io"
	"os"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgconf"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgconfig"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgmeta"
)

type configClient interface {
	wgconf.Client
	Close() error
}

var newConfigClient = func() (configClient, error) { return wgctrl.New() }
var configMetadataPath = wgmeta.DefaultPath

func showconf(args []string, _ io.Reader, out, errOut io.Writer) int {
	if len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(errOut, "用法: wg showconf <接口>")
		return 0
	}
	if len(args) != 1 {
		fmt.Fprintln(errOut, "用法: wg showconf <接口>")
		return 1
	}
	client, err := newConfigClient()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer client.Close()
	d, err := client.Device(args[0])
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if err := wgconf.AttachNames(d, configMetadataPath()); err != nil {
		fmt.Fprintf(errOut, "警告: 无法读取节点名称: %v\n", err)
	}
	if err := wgconfig.Encode(out, d); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

func syncconf(args []string, _ io.Reader, _ io.Writer, errOut io.Writer) int {
	return applyConfig(args, false, false, true, "syncconf", errOut)
}

func setconf(args []string, _ io.Reader, _ io.Writer, errOut io.Writer) int {
	return applyConfig(args, false, true, false, "setconf", errOut)
}

func addconf(args []string, _ io.Reader, _ io.Writer, errOut io.Writer) int {
	return applyConfig(args, true, false, false, "addconf", errOut)
}

func applyConfig(args []string, appendMode, replacePeers, syncPeers bool, command string, errOut io.Writer) int {
	usage := fmt.Sprintf("用法: wg %s <接口> <配置文件>", command)
	if len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(errOut, usage)
		return 0
	}
	if len(args) != 2 {
		fmt.Fprintln(errOut, usage)
		return 1
	}
	file, err := os.Open(args[1])
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer file.Close()
	cfg, err := wgconfig.Parse(file, appendMode)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	cfg.ReplacePeers = replacePeers
	client, err := newConfigClient()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer client.Close()
	if syncPeers {
		if err := wgconf.Sync(client, args[0], cfg, configMetadataPath()); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return 0
	}
	if err := wgconf.Apply(client, args[0], cfg, configMetadataPath()); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}
