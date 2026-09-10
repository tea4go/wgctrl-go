package wgquick

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Parse 从 r 读取 wg-quick 格式的配置并返回解析结果。
//
// 配置格式为 wg(8) setconf 格式的超集：除 [Interface]/[Peer] 的加密字段外，
// 还支持 wg-quick 特有的 [Interface] 网络字段 Address、DNS、MTU、Table、
// PreUp/PostUp/PreDown/PostDown 与 SaveConfig。
func Parse(r io.Reader) (*Config, error) {
	var cfg Config
	section := ""
	var peer *PeerConfig

	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := cleanLine(s.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			switch strings.ToLower(strings.TrimSpace(line[1 : len(line)-1])) {
			case "interface":
				section, peer = "interface", nil
			case "peer":
				cfg.Peers = append(cfg.Peers, PeerConfig{})
				peer = &cfg.Peers[len(cfg.Peers)-1]
				section = "peer"
			default:
				return nil, fmt.Errorf("line %d: unknown section %q", lineNo, line)
			}
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: invalid configuration line %q", lineNo, line)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)

		switch section {
		case "interface":
			if err := parseInterface(&cfg, key, value); err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
		case "peer":
			if peer == nil {
				return nil, fmt.Errorf("line %d: peer field %q outside a Peer section", lineNo, key)
			}
			if err := parsePeer(peer, key, value); err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
		default:
			return nil, fmt.Errorf("line %d: field %q outside a section", lineNo, key)
		}
	}
	if err := s.Err(); err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}

	for i := range cfg.Peers {
		if cfg.Peers[i].PublicKey == (wgtypes.Key{}) {
			return nil, fmt.Errorf("peer %d missing PublicKey", i+1)
		}
	}
	if cfg.PrivateKey == nil && cfg.ListenPort == nil && cfg.FirewallMark == nil &&
		len(cfg.Addresses) == 0 && len(cfg.Peers) == 0 {
		return nil, fmt.Errorf("no usable configuration found")
	}

	return &cfg, nil
}

func parseInterface(cfg *Config, key, value string) error {
	switch key {
	case "privatekey":
		k, err := wgtypes.ParseKey(value)
		if err != nil {
			return fmt.Errorf("parse PrivateKey %q: %w", value, err)
		}
		cfg.PrivateKey = &k
	case "listenport":
		n, err := parseUint(value, 65535)
		if err != nil {
			return fmt.Errorf("parse ListenPort %q: %w", value, err)
		}
		port := int(n)
		cfg.ListenPort = &port
	case "fwmark":
		n, err := parseFwmark(value)
		if err != nil {
			return fmt.Errorf("parse FwMark %q: %w", value, err)
		}
		mark := int(n)
		cfg.FirewallMark = &mark
	case "address":
		addrs, err := parseAddresses(value)
		if err != nil {
			return fmt.Errorf("parse Address %q: %w", value, err)
		}
		cfg.Addresses = append(cfg.Addresses, addrs...)
	case "dns":
		ips, err := parseIPs(value)
		if err != nil {
			return fmt.Errorf("parse DNS %q: %w", value, err)
		}
		cfg.DNS = append(cfg.DNS, ips...)
	case "mtu":
		n, err := parseUint(value, 65535)
		if err != nil {
			return fmt.Errorf("parse MTU %q: %w", value, err)
		}
		cfg.MTU = int(n)
	case "table":
		cfg.Table = value
	case "preup":
		cfg.PreUp = append(cfg.PreUp, value)
	case "postup":
		cfg.PostUp = append(cfg.PostUp, value)
	case "predown":
		cfg.PreDown = append(cfg.PreDown, value)
	case "postdown":
		cfg.PostDown = append(cfg.PostDown, value)
	case "saveconfig":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("parse SaveConfig %q: %w", value, err)
		}
		cfg.SaveConfig = b
	default:
		return fmt.Errorf("unknown interface field %q=%q", key, value)
	}
	return nil
}

func parsePeer(p *PeerConfig, key, value string) error {
	switch key {
	case "publickey":
		k, err := wgtypes.ParseKey(value)
		if err != nil {
			return fmt.Errorf("parse PublicKey %q: %w", value, err)
		}
		p.PublicKey = k
	case "presharedkey":
		k, err := wgtypes.ParseKey(value)
		if err != nil {
			return fmt.Errorf("parse PresharedKey %q: %w", value, err)
		}
		p.PresharedKey = &k
	case "allowedips":
		nets, err := parseNetworks(value)
		if err != nil {
			return fmt.Errorf("parse AllowedIPs %q: %w", value, err)
		}
		p.AllowedIPs = append(p.AllowedIPs, nets...)
	case "endpoint":
		ep, err := net.ResolveUDPAddr("udp", value)
		if err != nil {
			return fmt.Errorf("parse Endpoint %q: %w", value, err)
		}
		p.Endpoint = ep
	case "persistentkeepalive":
		d, err := parseKeepalive(value)
		if err != nil {
			return fmt.Errorf("parse PersistentKeepalive %q: %w", value, err)
		}
		p.PersistentKeepalive = &d
	default:
		return fmt.Errorf("unknown peer field %q=%q", key, value)
	}
	return nil
}

// cleanLine 去除首尾空白与 '#' 注释。注意：不能把 ';' 当注释，
// 因为钩子命令中常含分号。
func cleanLine(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if i := strings.IndexByte(line, '#'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	return line
}

func parseUint(value string, max uint64) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if n > max {
		return 0, fmt.Errorf("value %d exceeds maximum %d", n, max)
	}
	return n, nil
}

func parseFwmark(value string) (uint64, error) {
	if value == "off" {
		return 0, nil
	}
	base := 10
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 16
		value = value[2:]
	}
	return strconv.ParseUint(value, base, 32)
}

// parseAddresses 解析逗号分隔的接口地址，保留主机位（如 10.0.0.2/24）。
func parseAddresses(value string) ([]net.IPNet, error) {
	parts := strings.Split(value, ",")
	out := make([]net.IPNet, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		ip, ipn, err := net.ParseCIDR(p)
		if err != nil {
			return nil, err
		}
		out = append(out, net.IPNet{IP: ip, Mask: ipn.Mask})
	}
	return out, nil
}

// parseNetworks 解析逗号分隔的 CIDR 网络，主机位归零（如 10.0.0.0/24）。
func parseNetworks(value string) ([]net.IPNet, error) {
	parts := strings.Split(value, ",")
	out := make([]net.IPNet, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		_, ipn, err := net.ParseCIDR(p)
		if err != nil {
			return nil, err
		}
		out = append(out, *ipn)
	}
	return out, nil
}

func parseIPs(value string) ([]net.IP, error) {
	parts := strings.Split(value, ",")
	out := make([]net.IP, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		ip := net.ParseIP(p)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP %q", p)
		}
		out = append(out, ip)
	}
	return out, nil
}

func parseKeepalive(value string) (time.Duration, error) {
	if value == "off" {
		return 0, nil
	}
	n, err := parseUint(value, 65535)
	if err != nil {
		return 0, err
	}
	return time.Duration(n) * time.Second, nil
}
