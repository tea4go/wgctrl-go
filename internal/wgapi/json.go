package wgapi

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Device 是 Device 的 JSON 表示形式。
//
// 密钥以 wg(8) 使用的 base64 字符串表示，时间与间隔以秒为单位，
// 便于非 Go 客户端直接消费。
type Device struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	PrivateKey      string `json:"private_key,omitempty"`
	HasPrivateKey   bool   `json:"has_private_key"`
	PublicKey       string `json:"public_key,omitempty"`
	HasPublicKey    bool   `json:"has_public_key"`
	ListenPort      int    `json:"listen_port"`
	HasListenPort   bool   `json:"has_listen_port"`
	FirewallMark    int    `json:"fwmark"`
	HasFirewallMark bool   `json:"has_fwmark"`
	Peers           []Peer `json:"peers"`
}

// Peer 是 Peer 的 JSON 表示形式。
type Peer struct {
	Name                       string   `json:"name,omitempty"`
	PublicKey                  string   `json:"public_key"`
	PresharedKey               string   `json:"preshared_key,omitempty"`
	HasPresharedKey            bool     `json:"has_preshared_key"`
	Endpoint                   string   `json:"endpoint,omitempty"`
	HasEndpoint                bool     `json:"has_endpoint"`
	PersistentKeepaliveSeconds int64    `json:"persistent_keepalive_seconds,omitempty"`
	LastHandshakeTime          string   `json:"last_handshake_time,omitempty"`
	ReceiveBytes               int64    `json:"receive_bytes"`
	TransmitBytes              int64    `json:"transmit_bytes"`
	AllowedIPs                 []string `json:"allowed_ips,omitempty"`
	ProtocolVersion            int      `json:"protocol_version"`
}

func deviceToJSON(d *wgtypes.Device, hidePrivateKey bool) Device {
	out := Device{
		Name:            d.Name,
		Type:            d.Type.String(),
		HasPrivateKey:   d.HasPrivateKey,
		PublicKey:       d.PublicKey.String(),
		HasPublicKey:    d.HasPublicKey,
		ListenPort:      d.ListenPort,
		HasListenPort:   d.HasListenPort,
		FirewallMark:    d.FirewallMark,
		HasFirewallMark: d.HasFirewallMark,
		Peers:           make([]Peer, 0, len(d.Peers)),
	}
	if !hidePrivateKey {
		out.PrivateKey = d.PrivateKey.String()
	}
	for i := range d.Peers {
		out.Peers = append(out.Peers, peerToJSON(&d.Peers[i], hidePrivateKey))
	}
	return out
}

func peerToJSON(p *wgtypes.Peer, hidePrivateKey bool) Peer {
	out := Peer{
		PublicKey:       p.PublicKey.String(),
		HasPresharedKey: p.HasPresharedKey,
		HasEndpoint:     p.HasEndpoint,
		ReceiveBytes:    p.ReceiveBytes,
		TransmitBytes:   p.TransmitBytes,
		ProtocolVersion: p.ProtocolVersion,
	}
	if p.Name != nil {
		out.Name = *p.Name
	}
	if !hidePrivateKey {
		out.PresharedKey = p.PresharedKey.String()
	}
	if p.HasEndpoint && p.Endpoint != nil {
		out.Endpoint = formatEndpoint(p.Endpoint)
	}
	if p.PersistentKeepaliveInterval != 0 {
		out.PersistentKeepaliveSeconds = int64(p.PersistentKeepaliveInterval / time.Second)
	}
	if !p.LastHandshakeTime.IsZero() {
		out.LastHandshakeTime = p.LastHandshakeTime.UTC().Format(time.RFC3339)
	}
	if len(p.AllowedIPs) > 0 {
		out.AllowedIPs = make([]string, 0, len(p.AllowedIPs))
		for _, ip := range p.AllowedIPs {
			out.AllowedIPs = append(out.AllowedIPs, ip.String())
		}
	}
	return out
}

func formatEndpoint(endpoint *net.UDPAddr) string {
	host := endpoint.IP.String()
	if endpoint.Zone != "" {
		host += "%" + endpoint.Zone
	}
	return net.JoinHostPort(host, strconv.Itoa(endpoint.Port))
}

// Config 是 Config 的 JSON 表示形式，用于 POST /devices/{name}
// 直接应用结构化配置（等价于 wg set 的能力）。
//
// 指针字段在 JSON 中缺省时表示不修改对应设置；显式为 null
// 或零值时表示清除该设置，与 wgtypes.Config 的语义一致。
type Config struct {
	PrivateKey   *string      `json:"private_key,omitempty"`
	ListenPort   *int         `json:"listen_port,omitempty"`
	FirewallMark *int         `json:"fwmark,omitempty"`
	ReplacePeers bool         `json:"replace_peers,omitempty"`
	Peers        []PeerConfig `json:"peers,omitempty"`
}

// PeerConfig 是 PeerConfig 的 JSON 表示形式。
type PeerConfig struct {
	Name                       *string  `json:"name,omitempty"`
	PublicKey                  string   `json:"public_key"`
	Remove                     bool     `json:"remove,omitempty"`
	UpdateOnly                 bool     `json:"update_only,omitempty"`
	PresharedKey               *string  `json:"preshared_key,omitempty"`
	Endpoint                   *string  `json:"endpoint,omitempty"`
	PersistentKeepaliveSeconds *int64   `json:"persistent_keepalive_seconds,omitempty"`
	ReplaceAllowedIPs          bool     `json:"replace_allowed_ips,omitempty"`
	AllowedIPs                 []string `json:"allowed_ips,omitempty"`
}

// ParseConfig 将 JSON 配置转换为 wgtypes.Config。
func (c Config) ParseConfig() (wgtypes.Config, error) {
	out := wgtypes.Config{ReplacePeers: c.ReplacePeers}
	if c.PrivateKey != nil {
		key, err := wgtypes.ParseKey(*c.PrivateKey)
		if err != nil {
			return out, fmt.Errorf("private_key: %w", err)
		}
		out.PrivateKey = &key
	}
	out.ListenPort = c.ListenPort
	out.FirewallMark = c.FirewallMark
	for i, p := range c.Peers {
		pc, err := p.ParsePeerConfig()
		if err != nil {
			return out, fmt.Errorf("peers[%d]: %w", i, err)
		}
		out.Peers = append(out.Peers, pc)
	}
	return out, nil
}

// ParsePeerConfig 将 JSON 对等节点配置转换为 wgtypes.PeerConfig。
func (p PeerConfig) ParsePeerConfig() (wgtypes.PeerConfig, error) {
	out := wgtypes.PeerConfig{
		Name:              p.Name,
		Remove:            p.Remove,
		UpdateOnly:        p.UpdateOnly,
		ReplaceAllowedIPs: p.ReplaceAllowedIPs,
	}
	key, err := wgtypes.ParseKey(p.PublicKey)
	if err != nil {
		return out, fmt.Errorf("public_key: %w", err)
	}
	out.PublicKey = key
	if p.PresharedKey != nil {
		psk, err := wgtypes.ParseKey(*p.PresharedKey)
		if err != nil {
			return out, fmt.Errorf("preshared_key: %w", err)
		}
		out.PresharedKey = &psk
	}
	if p.Endpoint != nil {
		addr, err := net.ResolveUDPAddr("udp", *p.Endpoint)
		if err != nil {
			return out, fmt.Errorf("endpoint: %w", err)
		}
		out.Endpoint = addr
	}
	if p.PersistentKeepaliveSeconds != nil {
		d := time.Duration(*p.PersistentKeepaliveSeconds) * time.Second
		out.PersistentKeepaliveInterval = &d
	}
	for _, cidr := range p.AllowedIPs {
		_, ipnet, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil {
			return out, fmt.Errorf("allowed_ips: %w", err)
		}
		out.AllowedIPs = append(out.AllowedIPs, *ipnet)
	}
	return out, nil
}
