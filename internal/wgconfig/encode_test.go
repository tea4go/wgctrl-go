package wgconfig

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func testKeyValue(value byte) wgtypes.Key {
	var key wgtypes.Key
	key[0] = value
	return key
}

func mustCIDR(value string) net.IPNet {
	ip, network, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	network.IP = ip
	return *network
}

func TestEncode(t *testing.T) {
	privateKey := testKeyValue(1)
	presharedKey := testKeyValue(2)
	peerKey := testKeyValue(3)
	d := &wgtypes.Device{
		HasPrivateKey:   true,
		PrivateKey:      privateKey,
		HasListenPort:   true,
		ListenPort:      51820,
		HasFirewallMark: true,
		FirewallMark:    0xca6c,
		Peers: []wgtypes.Peer{
			{
				PublicKey:                      peerKey,
				HasPresharedKey:                true,
				PresharedKey:                   presharedKey,
				AllowedIPs:                     []net.IPNet{mustCIDR("10.0.0.0/24"), mustCIDR("2001:db8::/64")},
				HasEndpoint:                    true,
				Endpoint:                       &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 51820},
				HasPersistentKeepaliveInterval: true,
				PersistentKeepaliveInterval:    25 * time.Second,
			},
			{
				PublicKey: peerKey,
			},
		},
	}
	var got bytes.Buffer
	if err := Encode(&got, d); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := strings.Join([]string{
		"[Interface]",
		"ListenPort = 51820",
		"FwMark = 0xca6c",
		"PrivateKey = " + privateKey.String(),
		"",
		"[Peer]",
		"PublicKey = " + peerKey.String(),
		"PresharedKey = " + presharedKey.String(),
		"AllowedIPs = 10.0.0.0/24, 2001:db8::/64",
		"Endpoint = [2001:db8::1]:51820",
		"PersistentKeepalive = 25",
		"",
		"[Peer]",
		"PublicKey = " + peerKey.String(),
		"",
	}, "\n")
	if got.String() != want {
		t.Fatalf("output mismatch:\n got: %q\nwant: %q", got.String(), want)
	}
}

func TestEncodePeerName(t *testing.T) {
	name := "北京 #1 \"出口\""
	d := &wgtypes.Device{Peers: []wgtypes.Peer{{Name: &name, PublicKey: testKeyValue(1)}}}
	var got bytes.Buffer
	if err := Encode(&got, d); err != nil {
		t.Fatal(err)
	}
	want := "[Peer]\n# wgctrl-peer-name = \"北京 #1 \\\"出口\\\"\"\nPublicKey = "
	if !strings.Contains(got.String(), want) {
		t.Fatalf("name comment missing: %q", got.String())
	}

	parsed, err := Parse(strings.NewReader(got.String()), false)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Peers[0].Name == nil || *parsed.Peers[0].Name != name {
		t.Fatalf("round-trip name: %#v", parsed.Peers[0].Name)
	}
}

func TestEncodeOmitsEmptyPeerName(t *testing.T) {
	empty := ""
	d := &wgtypes.Device{Peers: []wgtypes.Peer{{Name: &empty, PublicKey: testKeyValue(1)}}}
	var got bytes.Buffer
	if err := Encode(&got, d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.String(), "wgctrl-peer-name") {
		t.Fatalf("empty name encoded: %q", got.String())
	}
}

func TestEncodeOmitsAbsentOptionalFields(t *testing.T) {
	d := &wgtypes.Device{Peers: []wgtypes.Peer{{PublicKey: testKeyValue(1), HasEndpoint: true, Endpoint: &net.UDPAddr{Port: 51820}}}}
	var got bytes.Buffer
	if err := Encode(&got, d); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := "[Interface]\n\n[Peer]\nPublicKey = " + d.Peers[0].PublicKey.String() + "\n"
	if got.String() != want {
		t.Fatalf("output = %q, want %q", got.String(), want)
	}
}
