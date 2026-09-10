package wgquick

import (
	"net"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestPeersFromDeviceSkipsZeroFields(t *testing.T) {
	psk, err := wgtypes.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub = pub.PublicKey()
	keepalive := 25 * time.Second

	dev := &wgtypes.Device{
		Peers: []wgtypes.Peer{
			// 未设置可选字段（零值但 Has 标志为真，模拟内核 dump 行为）。
			{
				PublicKey:                      pub,
				HasPresharedKey:                true,
				HasPersistentKeepaliveInterval: true,
				AllowedIPs:                     []net.IPNet{mustNet(t, "10.0.0.0/24")},
			},
			// 设置了可选字段。
			{
				PublicKey:                      psk.PublicKey(),
				HasPresharedKey:                true,
				PresharedKey:                   psk,
				HasPersistentKeepaliveInterval: true,
				PersistentKeepaliveInterval:    keepalive,
			},
		},
	}

	peers := peersFromDevice(dev)
	if len(peers) != 2 {
		t.Fatalf("len(peers) = %d, want 2", len(peers))
	}
	if peers[0].PresharedKey != nil {
		t.Errorf("peer[0].PresharedKey = %v, want nil (zero skipped)", peers[0].PresharedKey)
	}
	if peers[0].PersistentKeepalive != nil {
		t.Errorf("peer[0].PersistentKeepalive = %v, want nil (zero skipped)", peers[0].PersistentKeepalive)
	}
	if peers[1].PresharedKey == nil || *peers[1].PresharedKey != psk {
		t.Errorf("peer[1].PresharedKey = %v, want %v", peers[1].PresharedKey, psk)
	}
	if peers[1].PersistentKeepalive == nil || *peers[1].PersistentKeepalive != keepalive {
		t.Errorf("peer[1].PersistentKeepalive = %v, want %v", peers[1].PersistentKeepalive, keepalive)
	}
}

func TestApplyDeviceSkipsZeroInterfaceFields(t *testing.T) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	// HasListenPort/HasFirewallMark 为真但值为 0，模拟内核把未设置字段 dump 成零值。
	dev := &wgtypes.Device{
		HasPrivateKey:   true,
		PrivateKey:      priv,
		HasListenPort:   true,
		HasFirewallMark: true,
	}

	cfg := &Config{Name: "wg0"}
	applyDevice(cfg, dev)

	if cfg.PrivateKey == nil || *cfg.PrivateKey != priv {
		t.Error("PrivateKey not applied")
	}
	if cfg.ListenPort != nil {
		t.Errorf("ListenPort = %v, want nil (zero skipped)", *cfg.ListenPort)
	}
	if cfg.FirewallMark != nil {
		t.Errorf("FirewallMark = %v, want nil (zero skipped)", *cfg.FirewallMark)
	}
}
