package wgquick

import (
	"fmt"
	"io"
	"net"
	"strings"
)

// Strip 将配置中的加密字段输出为 wg(8) setconf 格式，
// 丢弃 wg-quick 特有的网络字段（Address、DNS、MTU、Table、钩子等）。
func Strip(w io.Writer, c *Config) error {
	hasInterface := c.PrivateKey != nil || c.ListenPort != nil || c.FirewallMark != nil
	if hasInterface {
		if _, err := fmt.Fprintln(w, "[Interface]"); err != nil {
			return err
		}
		if err := writeInterfaceCrypto(w, c); err != nil {
			return err
		}
	}
	for _, p := range c.Peers {
		if err := writePeer(w, p); err != nil {
			return err
		}
	}
	return nil
}

// Encode 将完整配置（加密字段 + wg-quick 网络字段）写回 wg-quick 格式。
// 用于 save：把设备的当前加密状态与配置文件中的网络字段合并写回。
func Encode(w io.Writer, c *Config) error {
	if _, err := fmt.Fprintln(w, "[Interface]"); err != nil {
		return err
	}
	if err := writeInterfaceCrypto(w, c); err != nil {
		return err
	}
	if len(c.Addresses) > 0 {
		if err := writeIPNetList(w, "Address", c.Addresses); err != nil {
			return err
		}
	}
	if len(c.DNS) > 0 {
		parts := make([]string, len(c.DNS))
		for i, ip := range c.DNS {
			parts[i] = ip.String()
		}
		if _, err := fmt.Fprintf(w, "DNS = %s\n", strings.Join(parts, ", ")); err != nil {
			return err
		}
	}
	if c.MTU > 0 {
		if _, err := fmt.Fprintf(w, "MTU = %d\n", c.MTU); err != nil {
			return err
		}
	}
	if c.Table != "" {
		if _, err := fmt.Fprintf(w, "Table = %s\n", c.Table); err != nil {
			return err
		}
	}
	for _, h := range c.PreUp {
		if _, err := fmt.Fprintf(w, "PreUp = %s\n", h); err != nil {
			return err
		}
	}
	for _, h := range c.PostUp {
		if _, err := fmt.Fprintf(w, "PostUp = %s\n", h); err != nil {
			return err
		}
	}
	for _, h := range c.PreDown {
		if _, err := fmt.Fprintf(w, "PreDown = %s\n", h); err != nil {
			return err
		}
	}
	for _, h := range c.PostDown {
		if _, err := fmt.Fprintf(w, "PostDown = %s\n", h); err != nil {
			return err
		}
	}
	if c.SaveConfig {
		if _, err := fmt.Fprintln(w, "SaveConfig = true"); err != nil {
			return err
		}
	}

	for _, p := range c.Peers {
		if err := writePeer(w, p); err != nil {
			return err
		}
	}
	return nil
}

func writeInterfaceCrypto(w io.Writer, c *Config) error {
	if c.PrivateKey != nil {
		if _, err := fmt.Fprintf(w, "PrivateKey = %s\n", c.PrivateKey.String()); err != nil {
			return err
		}
	}
	if c.ListenPort != nil {
		if _, err := fmt.Fprintf(w, "ListenPort = %d\n", *c.ListenPort); err != nil {
			return err
		}
	}
	if c.FirewallMark != nil {
		if _, err := fmt.Fprintf(w, "FwMark = %d\n", *c.FirewallMark); err != nil {
			return err
		}
	}
	return nil
}

func writeIPNetList(w io.Writer, key string, nets []net.IPNet) error {
	parts := make([]string, len(nets))
	for i, ipn := range nets {
		parts[i] = ipn.String()
	}
	_, err := fmt.Fprintf(w, "%s = %s\n", key, strings.Join(parts, ", "))
	return err
}

func writePeer(w io.Writer, p PeerConfig) error {
	if _, err := fmt.Fprintln(w, "\n[Peer]"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "PublicKey = %s\n", p.PublicKey.String()); err != nil {
		return err
	}
	if p.PresharedKey != nil {
		if _, err := fmt.Fprintf(w, "PresharedKey = %s\n", p.PresharedKey.String()); err != nil {
			return err
		}
	}
	if len(p.AllowedIPs) > 0 {
		if err := writeIPNetList(w, "AllowedIPs", p.AllowedIPs); err != nil {
			return err
		}
	}
	if p.Endpoint != nil {
		if _, err := fmt.Fprintf(w, "Endpoint = %s\n", p.Endpoint.String()); err != nil {
			return err
		}
	}
	if p.PersistentKeepalive != nil {
		if *p.PersistentKeepalive == 0 {
			if _, err := fmt.Fprintln(w, "PersistentKeepalive = off"); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(w, "PersistentKeepalive = %d\n", int(p.PersistentKeepalive.Seconds())); err != nil {
				return err
			}
		}
	}
	return nil
}
