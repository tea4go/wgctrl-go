package main

import (
	"fmt"
	"io"
	"os"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgconfig"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgmeta"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type configClient interface {
	Device(string) (*wgtypes.Device, error)
	ConfigureDevice(string, wgtypes.Config) error
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
	if err := attachNames(d, configMetadataPath()); err != nil {
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
		current, err := client.Device(args[0])
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		cfg.Peers = syncPeerConfigs(current, cfg.Peers)
	}
	if err := configureWithNames(client, args[0], cfg, configMetadataPath()); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

func syncPeerConfigs(current *wgtypes.Device, desired []wgtypes.PeerConfig) []wgtypes.PeerConfig {
	wanted := make(map[wgtypes.Key]bool, len(desired))
	out := append([]wgtypes.PeerConfig(nil), desired...)
	for _, peer := range desired {
		wanted[peer.PublicKey] = true
	}
	for _, peer := range current.Peers {
		if !wanted[peer.PublicKey] {
			out = append(out, wgtypes.PeerConfig{PublicKey: peer.PublicKey, Remove: true})
		}
	}
	return out
}

func configureWithNames(client configClient, interfaceName string, cfg wgtypes.Config, metadataPath string) error {
	native := cfg
	native.Peers = append([]wgtypes.PeerConfig(nil), cfg.Peers...)
	for i := range native.Peers {
		native.Peers[i].Name = nil
	}
	if err := client.ConfigureDevice(interfaceName, native); err != nil {
		return err
	}
	d, err := client.Device(interfaceName)
	if err != nil {
		return fmt.Errorf("WireGuard 配置已生效，但读取节点名称状态失败: %w", err)
	}
	exists := make(map[wgtypes.Key]bool, len(d.Peers))
	for _, peer := range d.Peers {
		exists[peer.PublicKey] = true
	}
	store := wgmeta.New(metadataPath)
	if err := store.Update(interfaceName, func(names map[wgtypes.Key]string) {
		if cfg.ReplacePeers {
			for key := range names {
				delete(names, key)
			}
		}
		for _, peer := range cfg.Peers {
			if peer.Remove {
				delete(names, peer.PublicKey)
				continue
			}
			if !exists[peer.PublicKey] {
				continue
			}
			if peer.Name != nil {
				if *peer.Name == "" {
					delete(names, peer.PublicKey)
				} else {
					names[peer.PublicKey] = *peer.Name
				}
			}
		}
	}); err != nil {
		return fmt.Errorf("WireGuard 配置已生效，但保存节点名称失败: %w", err)
	}
	return nil
}

func attachNames(d *wgtypes.Device, metadataPath string) error {
	names, err := wgmeta.New(metadataPath).Names(d.Name)
	if err != nil {
		return err
	}
	for i := range d.Peers {
		if name := names[d.Peers[i].PublicKey]; name != "" {
			d.Peers[i].Name = &name
		}
	}
	return nil
}
