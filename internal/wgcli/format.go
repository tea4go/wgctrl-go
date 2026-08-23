package wgcli

import (
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

func styled(color bool, code, value string) string {
	if !color {
		return value
	}
	return code + value + ansiReset
}

func bold(color bool, value string) string { return styled(color, ansiBold, value) }

func Pretty(w io.Writer, d *wgtypes.Device, now time.Time, showKeys, color bool) error {
	if color {
		if _, err := fmt.Fprint(w, ansiReset); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "%s: %s\n", styled(color, ansiGreen+ansiBold, "interface"), styled(color, ansiGreen, d.Name)); err != nil {
		return err
	}
	if d.HasPublicKey {
		if _, err := fmt.Fprintf(w, "  %s: %s\n", bold(color, "public key"), d.PublicKey); err != nil {
			return err
		}
	}
	if d.HasPrivateKey {
		if _, err := fmt.Fprintf(w, "  %s: %s\n", bold(color, "private key"), displayKey(d.PrivateKey, showKeys)); err != nil {
			return err
		}
	}
	if d.ListenPort != 0 {
		if _, err := fmt.Fprintf(w, "  %s: %d\n", bold(color, "listening port"), d.ListenPort); err != nil {
			return err
		}
	}
	if d.FirewallMark != 0 {
		if _, err := fmt.Fprintf(w, "  %s: 0x%x\n", bold(color, "fwmark"), d.FirewallMark); err != nil {
			return err
		}
	}
	peers := append([]wgtypes.Peer(nil), d.Peers...)
	sort.SliceStable(peers, func(i, j int) bool {
		ai, aj := peers[i].LastHandshakeTime, peers[j].LastHandshakeTime
		if ai.IsZero() != aj.IsZero() {
			return !ai.IsZero()
		}
		return ai.After(aj)
	})
	if len(peers) > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	for i := range peers {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		p := &peers[i]
		if _, err := fmt.Fprintf(w, "%s: %s\n", styled(color, ansiYellow+ansiBold, "peer"), styled(color, ansiYellow, p.PublicKey.String())); err != nil {
			return err
		}
		if p.HasPresharedKey {
			if _, err := fmt.Fprintf(w, "  %s: %s\n", bold(color, "preshared key"), displayKey(p.PresharedKey, showKeys)); err != nil {
				return err
			}
		}
		if p.Endpoint != nil && (p.Endpoint.IP.To4() != nil || p.Endpoint.IP.To16() != nil) {
			if _, err := fmt.Fprintf(w, "  %s: %s\n", bold(color, "endpoint"), endpoint(p.Endpoint)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "  %s: %s\n", bold(color, "allowed ips"), prettyAllowedIPs(p.AllowedIPs, color)); err != nil {
			return err
		}
		if !p.LastHandshakeTime.IsZero() {
			if _, err := fmt.Fprintf(w, "  %s: %s\n", bold(color, "latest handshake"), ago(now, p.LastHandshakeTime, color)); err != nil {
				return err
			}
		}
		if p.ReceiveBytes != 0 || p.TransmitBytes != 0 {
			if _, err := fmt.Fprintf(w, "  %s: %s received, %s sent\n", bold(color, "transfer"), byteCount(p.ReceiveBytes, color), byteCount(p.TransmitBytes, color)); err != nil {
				return err
			}
		}
		if p.PersistentKeepaliveInterval != 0 {
			if _, err := fmt.Fprintf(w, "  %s: every %s\n", bold(color, "persistent keepalive"), durationText(p.PersistentKeepaliveInterval, color)); err != nil {
				return err
			}
		}
	}
	return nil
}

func Field(w io.Writer, d *wgtypes.Device, field string, withInterface bool) error {
	prefix := func() error {
		if withInterface {
			_, err := fmt.Fprintf(w, "%s\t", d.Name)
			return err
		}
		return nil
	}
	keyLine := func(k wgtypes.Key) error {
		if err := prefix(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, "%s\n", k)
		return err
	}
	switch field {
	case "public-key":
		if err := prefix(); err != nil {
			return err
		}
		if d.HasPublicKey {
			_, err := fmt.Fprintf(w, "%s\n", d.PublicKey)
			return err
		}
		_, err := fmt.Fprintln(w, "(none)")
		return err
	case "private-key":
		if err := prefix(); err != nil {
			return err
		}
		_, err := fmt.Fprintln(w, displayKeyValue(d.PrivateKey, d.HasPrivateKey))
		return err
	case "listen-port":
		if err := prefix(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w, "%d\n", d.ListenPort)
		return err
	case "fwmark":
		if err := prefix(); err != nil {
			return err
		}
		if d.FirewallMark != 0 {
			_, err := fmt.Fprintf(w, "0x%x\n", d.FirewallMark)
			return err
		}
		_, err := fmt.Fprintln(w, "off")
		return err
	case "peers":
		for _, p := range d.Peers {
			if err := keyLine(p.PublicKey); err != nil {
				return err
			}
		}
		return nil
	case "preshared-keys":
		for _, p := range d.Peers {
			if err := prefix(); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\n", p.PublicKey, displayKeyValue(p.PresharedKey, p.HasPresharedKey)); err != nil {
				return err
			}
		}
		return nil
	case "endpoints":
		for _, p := range d.Peers {
			if err := prefix(); err != nil {
				return err
			}
			value := "(none)"
			if p.Endpoint != nil && (p.Endpoint.IP.To4() != nil || p.Endpoint.IP.To16() != nil) {
				value = endpoint(p.Endpoint)
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\n", p.PublicKey, value); err != nil {
				return err
			}
		}
		return nil
	case "allowed-ips":
		for _, p := range d.Peers {
			if err := prefix(); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\n", p.PublicKey, allowedIPs(p.AllowedIPs)); err != nil {
				return err
			}
		}
		return nil
	case "latest-handshakes":
		for _, p := range d.Peers {
			if err := prefix(); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "%s\t%d\n", p.PublicKey, handshakeSeconds(p.LastHandshakeTime)); err != nil {
				return err
			}
		}
		return nil
	case "transfer":
		for _, p := range d.Peers {
			if err := prefix(); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "%s\t%d\t%d\n", p.PublicKey, nonNegative(p.ReceiveBytes), nonNegative(p.TransmitBytes)); err != nil {
				return err
			}
		}
		return nil
	case "persistent-keepalive":
		for _, p := range d.Peers {
			if err := prefix(); err != nil {
				return err
			}
			value := "off"
			if p.PersistentKeepaliveInterval != 0 {
				value = fmt.Sprintf("%d", int(p.PersistentKeepaliveInterval/time.Second))
			}
			if _, err := fmt.Fprintf(w, "%s\t%s\n", p.PublicKey, value); err != nil {
				return err
			}
		}
		return nil
	case "dump":
		return dump(w, d, withInterface)
	default:
		return fmt.Errorf("invalid parameter: `%s'", field)
	}
}

func dump(w io.Writer, d *wgtypes.Device, withInterface bool) error {
	if withInterface {
		if _, err := fmt.Fprintf(w, "%s\t", d.Name); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "%s\t%s\t%d\t", displayKeyValue(d.PrivateKey, d.HasPrivateKey), displayKeyValue(d.PublicKey, d.HasPublicKey), d.ListenPort); err != nil {
		return err
	}
	mark := "off"
	if d.FirewallMark != 0 {
		mark = fmt.Sprintf("0x%x", d.FirewallMark)
	}
	if _, err := fmt.Fprintf(w, "%s\n", mark); err != nil {
		return err
	}
	for _, p := range d.Peers {
		if withInterface {
			if _, err := fmt.Fprintf(w, "%s\t", d.Name); err != nil {
				return err
			}
		}
		value := "(none)"
		if p.Endpoint != nil && (p.Endpoint.IP.To4() != nil || p.Endpoint.IP.To16() != nil) {
			value = endpoint(p.Endpoint)
		}
		ips := dumpAllowedIPs(p.AllowedIPs)
		keep := "off"
		if p.PersistentKeepaliveInterval != 0 {
			keep = fmt.Sprintf("%d", int(p.PersistentKeepaliveInterval/time.Second))
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n", p.PublicKey, displayKeyValue(p.PresharedKey, p.HasPresharedKey), value, ips, handshakeSeconds(p.LastHandshakeTime), nonNegative(p.ReceiveBytes), nonNegative(p.TransmitBytes), keep); err != nil {
			return err
		}
	}
	return nil
}

func displayKey(k wgtypes.Key, show bool) string {
	if show {
		return k.String()
	}
	return "(hidden)"
}
func displayKeyValue(k wgtypes.Key, present bool) string {
	if present {
		return k.String()
	}
	return "(none)"
}
func endpoint(a *net.UDPAddr) string { return a.String() }
func prettyAllowedIPs(ips []net.IPNet, color bool) string {
	if len(ips) == 0 {
		return "(none)"
	}
	values := make([]string, len(ips))
	for i := range ips {
		value := ips[i].String()
		slash := strings.LastIndexByte(value, '/')
		if color && slash >= 0 {
			value = value[:slash] + styled(true, ansiCyan, "/") + value[slash+1:]
		}
		values[i] = value
	}
	return strings.Join(values, ", ")
}

func allowedIPs(ips []net.IPNet) string {
	return joinedAllowedIPs(ips, " ")
}

func dumpAllowedIPs(ips []net.IPNet) string {
	return joinedAllowedIPs(ips, ",")
}

func joinedAllowedIPs(ips []net.IPNet, separator string) string {
	if len(ips) == 0 {
		return "(none)"
	}
	v := make([]string, len(ips))
	for i := range ips {
		v[i] = ips[i].String()
	}
	return strings.Join(v, separator)
}
func handshakeSeconds(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
func nonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
func durationText(d time.Duration, color bool) string {
	return prettyDuration(uint64(d/time.Second), color)
}
func prettyDuration(v uint64, color bool) string {
	var p []string
	for _, x := range []struct {
		n uint64
		s string
	}{{31536000, "year"}, {86400, "day"}, {3600, "hour"}, {60, "minute"}, {1, "second"}} {
		if v >= x.n {
			n := v / x.n
			v %= x.n
			s := x.s
			if n != 1 {
				s += "s"
			}
			p = append(p, fmt.Sprintf("%d %s", n, styled(color, ansiCyan, s)))
		}
	}
	return strings.Join(p, ", ")
}
func ago(now, then time.Time, color bool) string {
	if now.Equal(then) {
		return "Now"
	}
	if now.Before(then) {
		return "(" + styled(color, ansiRed, "System clock wound backward; connection problems may ensue.") + ")"
	}
	return prettyDuration(uint64(now.Sub(then)/time.Second), color) + " ago"
}
func byteCount(v int64, color bool) string {
	n := float64(nonNegative(v))
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	unit := styled(color, ansiCyan, units[i])
	if i == 0 {
		return fmt.Sprintf("%d %s", int64(n), unit)
	}
	return fmt.Sprintf("%.2f %s", n, unit)
}
