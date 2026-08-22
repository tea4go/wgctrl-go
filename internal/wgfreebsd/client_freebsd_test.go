//go:build freebsd
// +build freebsd

package wgfreebsd

import (
	"net"
	"testing"
	"unsafe"

	"golang.zx2c4.com/wireguard/wgctrl/internal/wgfreebsd/internal/nv"
	"golang.zx2c4.com/wireguard/wgctrl/internal/wgtest"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestUnparsePeerConfigAllowedIPOperations(t *testing.T) {
	m := unparsePeerConfig(wgtypes.PeerConfig{
		AllowedIPs: []net.IPNet{wgtest.MustCIDR("192.0.2.1/32")},
		AllowedIPOperations: []wgtypes.AllowedIPConfig{
			{IPNet: wgtest.MustCIDR("192.0.2.2/32"), Operation: wgtypes.AllowedIPSet},
			{IPNet: wgtest.MustCIDR("192.0.2.3/32"), Operation: wgtypes.AllowedIPAdd},
			{IPNet: wgtest.MustCIDR("192.0.2.4/32"), Operation: wgtypes.AllowedIPRemove},
		},
	})

	aips := m["allowed-ips"].([]nv.List)
	if len(aips) != 4 {
		t.Fatalf("unexpected allowed IP count: %d", len(aips))
	}

	want := []string{"192.0.2.1/32", "192.0.2.2/32", "192.0.2.3/32", "192.0.2.4/32"}
	for i, aip := range aips {
		if got := parseAllowedIP(aip).String(); got != want[i] {
			t.Fatalf("allowed IP %d: want %q, got %q", i, want[i], got)
		}
		flags, ok := aip["flags"]
		if i == 3 {
			if !ok || flags != uint64(1) {
				t.Fatalf("remove operation flags: want 1, got %v", flags)
			}
		} else if ok {
			t.Fatalf("allowed IP %d unexpectedly has flags: %v", i, flags)
		}
	}
}

func TestParseNVListFieldPresence(t *testing.T) {
	key := wgtest.MustPrivateKey()
	psk := wgtest.MustPresharedKey()

	peer := parsePeer(nv.List{
		"preshared-key":                 psk[:],
		"endpoint":                      unparseEndpoint(*wgtest.MustUDPAddr("192.0.2.1:51820")),
		"persistent-keepalive-interval": uint64(0),
	})
	if !peer.HasPresharedKey || !peer.HasEndpoint || !peer.HasPersistentKeepaliveInterval {
		t.Fatalf("peer field presence not preserved: %+v", peer)
	}

	pub := key.PublicKey()
	data := nv.List{
		"public-key":  pub[:],
		"private-key": key[:],
		"user-cookie": uint64(0),
		"listen-port": uint64(0),
	}
	buf, size, err := nv.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal device: %v", err)
	}
	device, err := parseDevice(unsafe.Slice(buf, size))
	if err != nil {
		t.Fatalf("failed to parse device: %v", err)
	}
	if !device.HasPublicKey || !device.HasPrivateKey || !device.HasFirewallMark || !device.HasListenPort {
		t.Fatalf("device field presence not preserved: %+v", device)
	}
}
