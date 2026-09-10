package wgquick

import (
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func mustNet(t *testing.T, s string) net.IPNet {
	t.Helper()
	_, ipn, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return *ipn
}

func mustAddr(t *testing.T, s string) net.IPNet {
	t.Helper()
	ip, ipn, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return net.IPNet{IP: ip, Mask: ipn.Mask}
}

func TestParse(t *testing.T) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.PublicKey()
	psk, err := wgtypes.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub2, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub2 = pub2.PublicKey()

	conf := fmt.Sprintf(`[Interface]
PrivateKey = %s
ListenPort = 51820
FwMark = 0xca6c
Address = 10.0.0.2/24, fd00::2/64
DNS = 1.1.1.1, 8.8.8.8
MTU = 1420
Table = 51820
PreUp = echo preup1
PreUp = echo preup2
PostUp = iptables -A FORWARD -i wg0 -j ACCEPT; iptables -A FORWARD -o wg0 -j ACCEPT
PostDown = echo postdown
SaveConfig = true

[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = 10.0.0.0/24, 10.0.1.5/32, ::/0
Endpoint = 1.2.3.4:51820
PersistentKeepalive = 25

[Peer]
PublicKey = %s
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = off
`, priv.String(), pub.String(), psk.String(), pub2.String())

	cfg, err := Parse(strings.NewReader(conf))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.PrivateKey == nil || *cfg.PrivateKey != priv {
		t.Errorf("PrivateKey = %v, want %v", cfg.PrivateKey, priv)
	}
	if cfg.ListenPort == nil || *cfg.ListenPort != 51820 {
		t.Errorf("ListenPort = %v, want 51820", cfg.ListenPort)
	}
	if cfg.FirewallMark == nil || *cfg.FirewallMark != 0xca6c {
		t.Errorf("FirewallMark = %v, want 0xca6c", cfg.FirewallMark)
	}
	wantAddr := []net.IPNet{mustAddr(t, "10.0.0.2/24"), mustAddr(t, "fd00::2/64")}
	if !reflect.DeepEqual(cfg.Addresses, wantAddr) {
		t.Errorf("Addresses = %v, want %v", cfg.Addresses, wantAddr)
	}
	wantDNS := []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8")}
	if !reflect.DeepEqual(cfg.DNS, wantDNS) {
		t.Errorf("DNS = %v, want %v", cfg.DNS, wantDNS)
	}
	if cfg.MTU != 1420 {
		t.Errorf("MTU = %d, want 1420", cfg.MTU)
	}
	if cfg.Table != "51820" {
		t.Errorf("Table = %q, want %q", cfg.Table, "51820")
	}
	if !reflect.DeepEqual(cfg.PreUp, []string{"echo preup1", "echo preup2"}) {
		t.Errorf("PreUp = %v", cfg.PreUp)
	}
	if !reflect.DeepEqual(cfg.PostUp, []string{"iptables -A FORWARD -i wg0 -j ACCEPT; iptables -A FORWARD -o wg0 -j ACCEPT"}) {
		t.Errorf("PostUp = %v", cfg.PostUp)
	}
	if !cfg.SaveConfig {
		t.Error("SaveConfig = false, want true")
	}

	if len(cfg.Peers) != 2 {
		t.Fatalf("len(Peers) = %d, want 2", len(cfg.Peers))
	}
	p0 := cfg.Peers[0]
	if p0.PublicKey != pub {
		t.Errorf("Peers[0].PublicKey = %v, want %v", p0.PublicKey, pub)
	}
	if p0.PresharedKey == nil || *p0.PresharedKey != psk {
		t.Errorf("Peers[0].PresharedKey = %v, want %v", p0.PresharedKey, psk)
	}
	wantAllowed := []net.IPNet{mustNet(t, "10.0.0.0/24"), mustNet(t, "10.0.1.5/32"), mustNet(t, "::/0")}
	if !reflect.DeepEqual(p0.AllowedIPs, wantAllowed) {
		t.Errorf("Peers[0].AllowedIPs = %v, want %v", p0.AllowedIPs, wantAllowed)
	}
	if p0.Endpoint == nil || p0.Endpoint.String() != "1.2.3.4:51820" {
		t.Errorf("Peers[0].Endpoint = %v, want 1.2.3.4:51820", p0.Endpoint)
	}
	if p0.PersistentKeepalive == nil || *p0.PersistentKeepalive != 25*time.Second {
		t.Errorf("Peers[0].PersistentKeepalive = %v, want 25s", p0.PersistentKeepalive)
	}

	p1 := cfg.Peers[1]
	if p1.PublicKey != pub2 {
		t.Errorf("Peers[1].PublicKey = %v, want %v", p1.PublicKey, pub2)
	}
	if !reflect.DeepEqual(p1.AllowedIPs, []net.IPNet{mustNet(t, "0.0.0.0/0")}) {
		t.Errorf("Peers[1].AllowedIPs = %v", p1.AllowedIPs)
	}
	if p1.PersistentKeepalive == nil || *p1.PersistentKeepalive != 0 {
		t.Errorf("Peers[1].PersistentKeepalive = %v, want 0 (off)", p1.PersistentKeepalive)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		conf string
		want string
	}{
		{"unknown section", "[Foo]\nPrivateKey = x\n", "unknown section"},
		{"unknown interface field", "[Interface]\nAddress2 = 1.2.3.4/32\n", "unknown interface field"},
		{"unknown peer field", "[Peer]\nPublicKey = " + zeroKey + "\nFoo = bar\n", "unknown peer field"},
		{"peer missing PublicKey", "[Interface]\nPrivateKey = " + zeroKey + "\n[Peer]\nAllowedIPs = 0.0.0.0/0\n", "missing PublicKey"},
		{"empty config", "[Interface]\n", "no usable configuration"},
		{"invalid private key", "[Interface]\nPrivateKey = notbase64!\n", "PrivateKey"},
		{"bad line", "[Interface]\nPrivateKey\n", "invalid configuration line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.conf))
			if err == nil {
				t.Fatalf("Parse succeeded, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

// zeroKey 是 32 字节全零的合法 base64 密钥。
const zeroKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func TestWireGuardConfig(t *testing.T) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.PublicKey()
	keepalive := 25 * time.Second

	cfg := &Config{
		PrivateKey:   &priv,
		ListenPort:   intPtr(51820),
		FirewallMark: intPtr(0xca6c),
		Peers: []PeerConfig{{
			PublicKey:           pub,
			AllowedIPs:          []net.IPNet{mustNet(t, "0.0.0.0/0")},
			PersistentKeepalive: &keepalive,
		}},
	}

	wgc := cfg.WireGuardConfig()
	if !wgc.ReplacePeers {
		t.Error("ReplacePeers = false, want true")
	}
	if wgc.PrivateKey == nil || *wgc.PrivateKey != priv {
		t.Errorf("PrivateKey = %v, want %v", wgc.PrivateKey, priv)
	}
	if wgc.ListenPort == nil || *wgc.ListenPort != 51820 {
		t.Errorf("ListenPort = %v, want 51820", wgc.ListenPort)
	}
	if wgc.FirewallMark == nil || *wgc.FirewallMark != 0xca6c {
		t.Errorf("FirewallMark = %v, want 0xca6c", wgc.FirewallMark)
	}
	if len(wgc.Peers) != 1 {
		t.Fatalf("len(Peers) = %d, want 1", len(wgc.Peers))
	}
	pc := wgc.Peers[0]
	if pc.PublicKey != pub {
		t.Errorf("Peer PublicKey = %v, want %v", pc.PublicKey, pub)
	}
	if !pc.ReplaceAllowedIPs {
		t.Error("ReplaceAllowedIPs = false, want true")
	}
	if pc.PersistentKeepaliveInterval == nil || *pc.PersistentKeepaliveInterval != keepalive {
		t.Errorf("PersistentKeepaliveInterval = %v, want %v", pc.PersistentKeepaliveInterval, keepalive)
	}
}

func intPtr(v int) *int { return &v }
