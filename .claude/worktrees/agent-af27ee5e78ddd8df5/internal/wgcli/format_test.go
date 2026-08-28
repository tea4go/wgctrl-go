package wgcli

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func key(value byte) wgtypes.Key {
	var k wgtypes.Key
	k[0] = value
	return k
}

func cidr(value string) net.IPNet {
	_, n, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return *n
}

func TestPrettyPeerName(t *testing.T) {
	name := "branch-office"
	d := &wgtypes.Device{Name: "wg0", Peers: []wgtypes.Peer{{Name: &name, PublicKey: key(1)}}}
	var out bytes.Buffer
	if err := Pretty(&out, d, time.Unix(1, 0), false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "  name: branch-office\n") || strings.Contains(out.String(), "  name: wg0") {
		t.Fatalf("unexpected peer name output: %q", out.String())
	}

	for _, absent := range []*string{nil, func() *string { value := ""; return &value }()} {
		d.Peers[0].Name = absent
		out.Reset()
		if err := Pretty(&out, d, time.Unix(1, 0), false, false); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "name:") {
			t.Fatalf("absent name displayed: %q", out.String())
		}
	}
}

func TestPretty(t *testing.T) {
	now := time.Unix(1000, 0)
	d := &wgtypes.Device{
		Name:          "wg0",
		HasPublicKey:  true,
		PublicKey:     key(1),
		HasPrivateKey: true,
		PrivateKey:    key(2),
		ListenPort:    51820,
		FirewallMark:  0xca6c,
		Peers: []wgtypes.Peer{
			{PublicKey: key(3), AllowedIPs: []net.IPNet{cidr("10.0.0.0/24")}, LastHandshakeTime: time.Unix(990, 0), ReceiveBytes: 1024, TransmitBytes: 2048},
			{PublicKey: key(4)},
		},
	}
	var out bytes.Buffer
	if err := Pretty(&out, d, now, false, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "interface: wg0") || !strings.Contains(out.String(), "private key: (hidden)") || !strings.Contains(out.String(), "peer: "+key(3).String()) || !strings.Contains(out.String(), "latest handshake: 10 seconds ago") {
		t.Fatalf("unexpected pretty output:\n%s", out.String())
	}
	if strings.Index(out.String(), key(3).String()) > strings.Index(out.String(), key(4).String()) {
		t.Fatal("peers were not sorted by latest handshake")
	}
}

func TestPrettyShowsKeysWhenRequested(t *testing.T) {
	d := &wgtypes.Device{HasPrivateKey: true, PrivateKey: key(1), Peers: []wgtypes.Peer{{PublicKey: key(2), HasPresharedKey: true, PresharedKey: key(3)}}}
	var out bytes.Buffer
	if err := Pretty(&out, d, time.Unix(1, 0), true, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "private key: "+key(1).String()) || !strings.Contains(out.String(), "preshared key: "+key(3).String()) {
		t.Fatalf("keys not shown:\n%s", out.String())
	}
}

func TestFieldDump(t *testing.T) {
	d := &wgtypes.Device{Name: "wg0", HasPublicKey: true, PublicKey: key(1), HasPrivateKey: true, PrivateKey: key(2), ListenPort: 51820, Peers: []wgtypes.Peer{{PublicKey: key(3), AllowedIPs: []net.IPNet{cidr("10.0.0.0/24")}}}}
	var out bytes.Buffer
	if err := Field(&out, d, "dump", false); err != nil {
		t.Fatal(err)
	}
	want := key(2).String() + "\t" + key(1).String() + "\t51820\toff\n" + key(3).String() + "\t(none)\t(none)\t10.0.0.0/24\t0\t0\t0\toff\n"
	if out.String() != want {
		t.Fatalf("dump = %q, want %q", out.String(), want)
	}
}

func TestPrettyPeerSpacingAndValuePresence(t *testing.T) {
	d := &wgtypes.Device{
		Name:         "wg0",
		ListenPort:   51820,
		FirewallMark: 0xca6c,
		Peers: []wgtypes.Peer{{
			PublicKey:                   key(1),
			Endpoint:                    &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 51820},
			PersistentKeepaliveInterval: 25 * time.Second,
		}},
	}
	var out bytes.Buffer
	if err := Pretty(&out, d, time.Unix(1, 0), false, false); err != nil {
		t.Fatal(err)
	}
	want := "interface: wg0\n" +
		"  listening port: 51820\n" +
		"  fwmark: 0xca6c\n\n" +
		"peer: " + key(1).String() + "\n" +
		"  endpoint: 192.0.2.1:51820\n" +
		"  allowed ips: (none)\n" +
		"  persistent keepalive: every 25 seconds\n"
	if out.String() != want {
		t.Fatalf("pretty = %q, want %q", out.String(), want)
	}
}

func TestAllowedIPSeparators(t *testing.T) {
	d := &wgtypes.Device{
		Name: "wg0",
		Peers: []wgtypes.Peer{{
			PublicKey:  key(1),
			AllowedIPs: []net.IPNet{cidr("10.0.0.0/24"), cidr("2001:db8::/32")},
		}},
	}

	var out bytes.Buffer
	if err := Field(&out, d, "allowed-ips", false); err != nil {
		t.Fatal(err)
	}
	want := key(1).String() + "\t10.0.0.0/24 2001:db8::/32\n"
	if out.String() != want {
		t.Fatalf("allowed-ips = %q, want %q", out.String(), want)
	}

	out.Reset()
	if err := Field(&out, d, "dump", false); err != nil {
		t.Fatal(err)
	}
	want = "(none)\t(none)\t0\toff\n" + key(1).String() + "\t(none)\t(none)\t10.0.0.0/24,2001:db8::/32\t0\t0\t0\toff\n"
	if out.String() != want {
		t.Fatalf("dump = %q, want %q", out.String(), want)
	}
}

func TestPrettyColored(t *testing.T) {
	now := time.Unix(1000, 0)
	d := &wgtypes.Device{
		Name:       "wg0",
		ListenPort: 51820,
		Peers: []wgtypes.Peer{{
			PublicKey:                   key(1),
			AllowedIPs:                  []net.IPNet{cidr("10.0.0.0/24")},
			LastHandshakeTime:           time.Unix(990, 0),
			ReceiveBytes:                1024,
			TransmitBytes:               2048,
			PersistentKeepaliveInterval: 25 * time.Second,
		}},
	}
	var out bytes.Buffer
	if err := Pretty(&out, d, now, false, true); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"\x1b[0m\x1b[32m\x1b[1minterface\x1b[0m: \x1b[32mwg0\x1b[0m\n",
		"  \x1b[1mlistening port\x1b[0m: 51820\n",
		"\x1b[33m\x1b[1mpeer\x1b[0m: \x1b[33m" + key(1).String() + "\x1b[0m\n",
		"10.0.0.0\x1b[36m/\x1b[0m24",
		"10 \x1b[36mseconds\x1b[0m ago",
		"1.00 \x1b[36mKiB\x1b[0m received, 2.00 \x1b[36mKiB\x1b[0m sent",
		"every 25 \x1b[36mseconds\x1b[0m",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("colored pretty output missing %q:\n%q", want, out.String())
		}
	}
}

func TestPrettyClockWoundBackwardColored(t *testing.T) {
	d := &wgtypes.Device{Name: "wg0", Peers: []wgtypes.Peer{{PublicKey: key(1), LastHandshakeTime: time.Unix(2, 0)}}}
	var out bytes.Buffer
	if err := Pretty(&out, d, time.Unix(1, 0), false, true); err != nil {
		t.Fatal(err)
	}
	want := "(\x1b[31mSystem clock wound backward; connection problems may ensue.\x1b[0m)"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("colored clock warning missing: %q", out.String())
	}
}

func TestFieldRejectsUnknown(t *testing.T) {
	if err := Field(&bytes.Buffer{}, &wgtypes.Device{}, "unknown", false); err == nil {
		t.Fatal("unknown field accepted")
	}
}
