package wgquick

import (
	"net"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestEncodeRoundTrip(t *testing.T) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.PublicKey()
	keepalive := 25 * time.Second

	cfg := &Config{
		PrivateKey: &priv,
		ListenPort: intPtr(51820),
		Addresses:  []net.IPNet{mustAddr(t, "10.0.0.2/24")},
		DNS:        []net.IP{net.ParseIP("1.1.1.1")},
		MTU:        1420,
		Table:      "51820",
		PreUp:      []string{"echo preup"},
		SaveConfig: true,
		Peers: []PeerConfig{{
			PublicKey:           pub,
			AllowedIPs:          []net.IPNet{mustNet(t, "10.0.0.0/24")},
			PersistentKeepalive: &keepalive,
		}},
	}

	var b strings.Builder
	if err := Encode(&b, cfg); err != nil {
		t.Fatal(err)
	}

	got, err := Parse(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("re-parse encoded config: %v\n%s", err, b.String())
	}

	if got.PrivateKey == nil || *got.PrivateKey != priv {
		t.Error("round-trip PrivateKey mismatch")
	}
	if got.ListenPort == nil || *got.ListenPort != 51820 {
		t.Error("round-trip ListenPort mismatch")
	}
	if len(got.Addresses) != 1 || got.Addresses[0].String() != "10.0.0.2/24" {
		t.Errorf("round-trip Addresses = %v", got.Addresses)
	}
	if len(got.DNS) != 1 || !got.DNS[0].Equal(net.ParseIP("1.1.1.1")) {
		t.Errorf("round-trip DNS = %v", got.DNS)
	}
	if got.MTU != 1420 || got.Table != "51820" || !got.SaveConfig {
		t.Errorf("round-trip network fields mismatch: MTU=%d Table=%q Save=%v", got.MTU, got.Table, got.SaveConfig)
	}
	if !strings.Contains(b.String(), "PreUp = echo preup") {
		t.Error("Encode missing PreUp hook")
	}
	if len(got.Peers) != 1 || got.Peers[0].PublicKey != pub {
		t.Error("round-trip peers mismatch")
	}
	if got.Peers[0].PersistentKeepalive == nil || *got.Peers[0].PersistentKeepalive != keepalive {
		t.Error("round-trip keepalive mismatch")
	}
}

func TestStripDropsNetworkFields(t *testing.T) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		PrivateKey: &priv,
		Addresses:  []net.IPNet{mustAddr(t, "10.0.0.2/24")},
		DNS:        []net.IP{net.ParseIP("1.1.1.1")},
		MTU:        1420,
		Table:      "51820",
		PostUp:     []string{"echo up"},
	}

	var b strings.Builder
	if err := Strip(&b, cfg); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, banned := range []string{"Address", "DNS", "MTU", "Table", "PostUp", "PreUp", "PreDown", "PostDown", "SaveConfig"} {
		if strings.Contains(out, banned) {
			t.Errorf("Strip output unexpectedly contains %q:\n%s", banned, out)
		}
	}
	if !strings.Contains(out, "PrivateKey = ") {
		t.Errorf("Strip output missing PrivateKey:\n%s", out)
	}
}
