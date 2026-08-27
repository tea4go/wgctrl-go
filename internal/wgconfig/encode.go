package wgconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Encode writes a wg(8) showconf-compatible device configuration.
func Encode(w io.Writer, d *wgtypes.Device) error {
	return encode(w, d)
}

// EncodeColor 写入可选 ANSI 颜色的 wg(8) showconf 兼容配置。
func EncodeColor(w io.Writer, d *wgtypes.Device, color bool) error {
	if !color {
		return encode(w, d)
	}
	var b bytes.Buffer
	if err := encode(&b, d); err != nil {
		return err
	}
	for _, line := range strings.SplitAfter(b.String(), "\n") {
		trimmed := strings.TrimSuffix(line, "\n")
		suffix := strings.TrimPrefix(line, trimmed)
		switch {
		case strings.HasPrefix(trimmed, "["):
			trimmed = "\x1b[36;1m" + trimmed + "\x1b[0m"
		case strings.Contains(trimmed, " = "):
			parts := strings.SplitN(trimmed, " = ", 2)
			trimmed = "\x1b[1m" + parts[0] + "\x1b[0m = \x1b[32m" + parts[1] + "\x1b[0m"
		}
		if _, err := io.WriteString(w, trimmed+suffix); err != nil {
			return err
		}
	}
	return nil
}

func encode(w io.Writer, d *wgtypes.Device) error {
	if _, err := fmt.Fprintln(w, "[Interface]"); err != nil {
		return err
	}
	if d.ListenPort != 0 {
		if _, err := fmt.Fprintf(w, "ListenPort = %d\n", d.ListenPort); err != nil {
			return err
		}
	}
	if d.FirewallMark != 0 {
		if _, err := fmt.Fprintf(w, "FwMark = 0x%x\n", d.FirewallMark); err != nil {
			return err
		}
	}
	if d.HasPrivateKey {
		if _, err := fmt.Fprintf(w, "PrivateKey = %s\n", d.PrivateKey); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for i := range d.Peers {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := encodePeer(w, &d.Peers[i]); err != nil {
			return err
		}
	}
	return nil
}

func encodePeer(w io.Writer, p *wgtypes.Peer) error {
	if _, err := fmt.Fprintln(w, "[Peer]"); err != nil {
		return err
	}
	if p.Name != nil && *p.Name != "" {
		if err := validatePeerName(*p.Name); err != nil {
			return fmt.Errorf("invalid peer name: %w", err)
		}
		encoded, err := json.Marshal(*p.Name)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "# wgctrl-peer-name = %s\n", encoded); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "PublicKey = %s\n", p.PublicKey); err != nil {
		return err
	}
	if p.HasPresharedKey {
		if _, err := fmt.Fprintf(w, "PresharedKey = %s\n", p.PresharedKey); err != nil {
			return err
		}
	}
	if len(p.AllowedIPs) > 0 {
		values := make([]string, 0, len(p.AllowedIPs))
		for _, allowedIP := range p.AllowedIPs {
			values = append(values, allowedIP.String())
		}
		if _, err := fmt.Fprintf(w, "AllowedIPs = %s\n", strings.Join(values, ", ")); err != nil {
			return err
		}
	}
	if p.HasEndpoint && p.Endpoint != nil && (p.Endpoint.IP.To4() != nil || p.Endpoint.IP.To16() != nil) {
		if _, err := fmt.Fprintf(w, "Endpoint = %s\n", formatEndpoint(p.Endpoint)); err != nil {
			return err
		}
	}
	if p.PersistentKeepaliveInterval != 0 {
		if _, err := fmt.Fprintf(w, "PersistentKeepalive = %d\n", int64(p.PersistentKeepaliveInterval/time.Second)); err != nil {
			return err
		}
	}
	return nil
}

func formatEndpoint(endpoint *net.UDPAddr) string {
	host := endpoint.IP.String()
	if endpoint.Zone != "" {
		host += "%" + endpoint.Zone
	}
	return net.JoinHostPort(host, strconv.Itoa(endpoint.Port))
}
