// Package wgquick 实现 wg-quick 的核心逻辑：解析 wg-quick 格式的配置文件，
// 并通过 wgctrl 库 + Linux 网络工具完成接口生命周期与网络集成
// （创建接口、加密配置、地址、路由、DNS 与钩子）。
package wgquick

import (
	"net"
	"strconv"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// DefaultTable 是 wg-quick 为默认路由（0.0.0.0/0、::/0）隔离
// 所使用的默认路由表编号。
const DefaultTable = 51820

// Config 是 wg-quick 格式配置文件的解析结果。
//
// 它在 wgtypes.Config（仅加密配置）的基础上，额外承载 wg-quick 特有的
// 网络配置字段：地址、DNS、MTU、路由表、钩子与 SaveConfig。
type Config struct {
	// Name 是接口名，由调用方根据配置文件名推导。
	Name string

	// 加密配置（[Interface]）
	PrivateKey   *wgtypes.Key
	ListenPort   *int
	FirewallMark *int

	// 网络配置（[Interface]）
	Addresses  []net.IPNet
	DNS        []net.IP
	MTU        int
	Table      string // 空串、数字字符串，或 "auto"
	PreUp      []string
	PostUp     []string
	PreDown    []string
	PostDown   []string
	SaveConfig bool

	// 对等节点配置（[Peer]）
	Peers []PeerConfig
}

// PeerConfig 是 wg-quick 配置中单个 [Peer] 的解析结果。
type PeerConfig struct {
	PublicKey           wgtypes.Key
	PresharedKey        *wgtypes.Key
	AllowedIPs          []net.IPNet
	Endpoint            *net.UDPAddr
	PersistentKeepalive *time.Duration
}

// WireGuardConfig 将加密配置转换为 wgctrl 可用的 wgtypes.Config。
// 采用 syncconf 语义：替换全部 peer，替换每个 peer 的全部 AllowedIPs。
func (c *Config) WireGuardConfig() wgtypes.Config {
	cfg := wgtypes.Config{ReplacePeers: true}

	if c.PrivateKey != nil {
		key := *c.PrivateKey
		cfg.PrivateKey = &key
	}
	if c.ListenPort != nil {
		port := *c.ListenPort
		cfg.ListenPort = &port
	}
	if c.FirewallMark != nil {
		mark := *c.FirewallMark
		cfg.FirewallMark = &mark
	}

	for _, p := range c.Peers {
		pc := wgtypes.PeerConfig{
			PublicKey:         p.PublicKey,
			ReplaceAllowedIPs: true,
			AllowedIPs:        append([]net.IPNet(nil), p.AllowedIPs...),
		}
		if p.PresharedKey != nil {
			key := *p.PresharedKey
			pc.PresharedKey = &key
		}
		if p.Endpoint != nil {
			ep := *p.Endpoint
			pc.Endpoint = &ep
		}
		if p.PersistentKeepalive != nil {
			d := *p.PersistentKeepalive
			pc.PersistentKeepaliveInterval = &d
		}
		cfg.Peers = append(cfg.Peers, pc)
	}

	return cfg
}

// routeTable 返回默认路由隔离使用的路由表编号，同时也是隔离所用的 fwmark。
// 二者必须一致，否则 `not fwmark` 规则无法正确隔离 WireGuard 自身的 UDP 流量。
//
// 优先级：显式数字 Table > 显式 FwMark > DefaultTable。
func (c *Config) routeTable() int {
	if c.Table != "" && c.Table != "auto" && c.Table != "off" {
		if n, err := strconv.Atoi(c.Table); err == nil {
			return n
		}
	}
	if c.FirewallMark != nil {
		return *c.FirewallMark
	}
	return DefaultTable
}

// routeMode 返回路由处理模式：
//   - "off"：不添加任何路由（Table = off）
//   - "explicit"：所有路由都进显式指定的表，规则由用户自行管理
//   - "auto"：默认路由做隔离（表 + fwmark 规则），普通路由进主表
func (c *Config) routeMode() string {
	if c.Table == "off" {
		return "off"
	}
	if c.Table != "" && c.Table != "auto" {
		return "explicit"
	}
	return "auto"
}

// hasDefaultRoute 报告 AllowedIPs 中是否包含默认路由（0.0.0.0/0 或 ::/0）。
func (c *Config) hasDefaultRoute() bool {
	for _, p := range c.Peers {
		for _, ipn := range p.AllowedIPs {
			if isDefaultRoute(ipn) {
				return true
			}
		}
	}
	return false
}

// isDefaultRoute 判断 CIDR 是否为 IPv4/IPv6 默认路由。
func isDefaultRoute(ipn net.IPNet) bool {
	ones, bits := ipn.Mask.Size()
	return ones == 0 && (bits == 32 || bits == 128)
}
