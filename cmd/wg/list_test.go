package main

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type fakeListClient struct {
	devices []*wgtypes.Device
	device  *wgtypes.Device
	err     error
}

func (c *fakeListClient) Devices() ([]*wgtypes.Device, error)    { return c.devices, c.err }
func (c *fakeListClient) Device(string) (*wgtypes.Device, error) { return c.device, c.err }
func (c *fakeListClient) Close() error                           { return nil }

func testPeer(ip string, endpoint *net.UDPAddr) wgtypes.Peer {
	ipnet := net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(24, 32)}
	return wgtypes.Peer{
		PublicKey:  wgtypes.Key{},
		AllowedIPs: []net.IPNet{ipnet},
		Endpoint:   endpoint,
	}
}

func TestListPeerKey(t *testing.T) {
	// 全零密钥的 base64 为 44 个字符，摘要应为前 5 位 + "***" + 后 4 位。
	if got, want := listPeerKey(wgtypes.Key{}), "AAAAA***AAA="; got != want {
		t.Fatalf("listPeerKey()=%q, want %q", got, want)
	}
}

func TestListConfigPath(t *testing.T) {
	oldDir := listConfigDir
	t.Cleanup(func() { listConfigDir = oldDir })
	listConfigDir = func() string { return "/etc/wireguard" }

	if got, want := listConfigPath("wg0"), "/etc/wireguard/wg0.conf"; got != want {
		t.Fatalf("listConfigPath()=%q, want %q", got, want)
	}
}

func TestSortedPeers(t *testing.T) {
	peers := []wgtypes.Peer{
		testPeer("10.0.0.10", nil),
		testPeer("10.0.0.2", nil),
		testPeer("10.0.0.100", nil),
		testPeer("10.0.0.1", nil),
	}
	got := sortedPeers(peers)
	want := []string{"10.0.0.1/24", "10.0.0.2/24", "10.0.0.10/24", "10.0.0.100/24"}
	for i, ip := range want {
		if got[i].AllowedIPs[0].String() != ip {
			t.Fatalf("sortedPeers()[%d]=%q, want %q", i, got[i].AllowedIPs[0].String(), ip)
		}
	}
	// 原 slice 不应被修改
	if peers[0].AllowedIPs[0].String() != "10.0.0.10/24" {
		t.Fatalf("输入 slice 被修改: peers[0]=%q", peers[0].AllowedIPs[0].String())
	}
}

func TestListAllowedIP(t *testing.T) {
	if got := listAllowedIP(testPeer("192.168.190.100", nil)); got != "192.168.190.100/24" {
		t.Fatalf("listAllowedIP()=%q", got)
	}
	if got := listAllowedIP(wgtypes.Peer{}); got != "(none)" {
		t.Fatalf("listAllowedIP(empty)=%q", got)
	}
}

func TestListEndpoint(t *testing.T) {
	ep := &net.UDPAddr{IP: net.ParseIP("101.133.133.127"), Port: 8357}
	if got := listEndpoint(testPeer("192.168.190.100", ep)); got != "101.133.133.127:8357" {
		t.Fatalf("listEndpoint()=%q", got)
	}
	if got := listEndpoint(wgtypes.Peer{}); got != "(none)" {
		t.Fatalf("listEndpoint(empty)=%q", got)
	}
}

func TestListDevice(t *testing.T) {
	old := newListClient
	t.Cleanup(func() { newListClient = old })
	oldDir := listConfigDir
	t.Cleanup(func() { listConfigDir = oldDir })
	listConfigDir = func() string { return "/etc/wireguard" }

	device := &wgtypes.Device{
		Name:         "wg0",
		PublicKey:    wgtypes.Key{},
		HasPublicKey: true,
		ListenPort:   40993,
		Peers: []wgtypes.Peer{
			testPeer("192.168.190.100", &net.UDPAddr{IP: net.ParseIP("101.133.133.127"), Port: 8357}),
			testPeer("192.168.190.0", nil),
		},
	}
	newListClient = func() (showClient, error) {
		return &fakeListClient{device: device}, nil
	}

	var out, errOut bytes.Buffer
	if code := list([]string{"wg0"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut.String())
	}
	want := "interface: wg0\n" +
		"  config file: /etc/wireguard/wg0.conf\n" +
		"  public key: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n" +
		"  private key: (hidden)\n" +
		"  listening port: 40993\n" +
		"\n" +
		"peer: 2\n" +
		"  public key   allowed ips        endpoint\n" +
		"  AAAAA***AAA= 192.168.190.0/24   (none)\n" +
		"  AAAAA***AAA= 192.168.190.100/24 101.133.133.127:8357\n"
	if out.String() != want {
		t.Fatalf("unexpected output:\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
	}
	if errOut.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errOut.String())
	}
}

func TestListAllDevicesSeparatedByBlankLine(t *testing.T) {
	old := newListClient
	t.Cleanup(func() { newListClient = old })

	newListClient = func() (showClient, error) {
		return &fakeListClient{devices: []*wgtypes.Device{{Name: "wg0"}, {Name: "wg1"}}}, nil
	}

	var out, errOut bytes.Buffer
	if code := list(nil, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "interface: wg0\n") ||
		!strings.Contains(out.String(), "\n\ninterface: wg1\n") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestListError(t *testing.T) {
	old := newListClient
	t.Cleanup(func() { newListClient = old })

	newListClient = func() (showClient, error) { return &fakeListClient{err: errors.New("boom")}, nil }

	var out, errOut bytes.Buffer
	if code := list(nil, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "boom") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
