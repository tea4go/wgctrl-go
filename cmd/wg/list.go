package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgmeta"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var newListClient = func() (showClient, error) { return wgctrl.New() }
var listConfigDir = wgmeta.DefaultPath

// listConfigPath 返回指定接口对应的 WireGuard 配置文件路径。
func listConfigPath(iface string) string {
	return filepath.Join(listConfigDir(), iface+".conf")
}

// list 输出所有（或指定）WireGuard 接口的简介信息。
func list(args []string, _ io.Reader, out, errOut io.Writer) int {
	if len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(errOut, "用法: wg --list [<接口>]")
		return 0
	}
	if len(args) > 1 {
		fmt.Fprintln(errOut, "用法: wg --list [<接口>]")
		return 1
	}

	client, err := newListClient()
	if err != nil {
		fmt.Fprintf(errOut, "无法创建客户端: %v\n", err)
		return 1
	}
	defer client.Close()

	if len(args) == 0 {
		devices, err := client.Devices()
		if err != nil {
			fmt.Fprintf(errOut, "无法列出接口: %v\n", err)
			return 1
		}
		for i, device := range devices {
			if i > 0 {
				fmt.Fprintln(out)
			}
			if err := prettyList(out, device); err != nil {
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
	if err := prettyList(out, device); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

// prettyList 将单个设备以简介格式写入 w。
func prettyList(w io.Writer, d *wgtypes.Device) error {
	if _, err := fmt.Fprintf(w, "interface: %s\n", d.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  config file: %s\n", listConfigPath(d.Name)); err != nil {
		return err
	}
	if d.HasPublicKey {
		if _, err := fmt.Fprintf(w, "  public key: %s\n", d.PublicKey); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "  private key: (hidden)"); err != nil {
		return err
	}
	if d.ListenPort != 0 {
		if _, err := fmt.Fprintf(w, "  listening port: %d\n", d.ListenPort); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "\npeer: %d\n", len(d.Peers)); err != nil {
		return err
	}
	if len(d.Peers) > 0 {
		if _, err := fmt.Fprintf(w, "  %-12s %-18s %s\n", "public key", "allowed ips", "endpoint"); err != nil {
			return err
		}
		for _, p := range sortedPeers(d.Peers) {
			if _, err := fmt.Fprintf(w, "  %-12s %-18s %s\n", listPeerKey(p.PublicKey), listAllowedIP(p), listEndpoint(p)); err != nil {
				return err
			}
		}
	}
	return nil
}

// sortedPeers 返回按首个允许 IP 的第 4 段（IPv4 最后一个八位组）数字升序排列的
// peers 副本；无允许 IP 或非 IPv4 的对等节点排在最后。
func sortedPeers(peers []wgtypes.Peer) []wgtypes.Peer {
	out := make([]wgtypes.Peer, len(peers))
	copy(out, peers)
	sort.SliceStable(out, func(i, j int) bool {
		return listPeerLastOctet(out[i]) < listPeerLastOctet(out[j])
	})
	return out
}

// listPeerLastOctet 返回对等节点首个允许 IP 的最后一个八位组（IPv4 第 4 段），
// 用于数字升序排序；无允许 IP 或非 IPv4 时返回 256 以确保排在最后。
func listPeerLastOctet(p wgtypes.Peer) int {
	if len(p.AllowedIPs) == 0 {
		return 256
	}
	if v4 := p.AllowedIPs[0].IP.To4(); v4 != nil {
		return int(v4[3])
	}
	return 256
}

// listPeerKey 返回对等节点公钥的摘要形式：前 5 个字符 + "***" + 后 4 个字符。
func listPeerKey(k wgtypes.Key) string {
	s := k.String()
	if len(s) > 12 {
		return s[:5] + "***" + s[len(s)-4:]
	}
	return s
}

// listAllowedIP 返回对等节点的首个允许 IP；若无则返回 "(none)"。
func listAllowedIP(p wgtypes.Peer) string {
	if len(p.AllowedIPs) == 0 {
		return "(none)"
	}
	return p.AllowedIPs[0].String()
}

// listEndpoint 返回对等节点的端点地址；若无则返回 "(none)"。
func listEndpoint(p wgtypes.Peer) string {
	if p.Endpoint == nil {
		return "(none)"
	}
	return p.Endpoint.String()
}
