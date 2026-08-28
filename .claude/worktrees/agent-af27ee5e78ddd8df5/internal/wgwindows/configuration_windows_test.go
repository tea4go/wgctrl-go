package wgwindows

import (
	"net"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"golang.zx2c4.com/wireguard/wgctrl/internal/wgwindows/internal/ioctl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestBuildConfigurationBufferAlignment(t *testing.T) {
	interfaze, size := buildConfiguration(wgtypes.Config{})
	if size == 0 {
		t.Fatal("buildConfiguration returned an empty buffer")
	}
	if address := uintptr(unsafe.Pointer(interfaze)); address%8 != 0 {
		t.Fatalf("configuration buffer address is not 8-byte aligned: %#x", address)
	}
}

func TestConfigurationFieldPresence(t *testing.T) {
	interfaze := &ioctl.Interface{
		Flags: ioctl.InterfaceHasPrivateKey |
			ioctl.InterfaceHasPublicKey |
			ioctl.InterfaceHasListenPort,
		PeerCount: 1,
	}
	peer := &ioctl.Peer{
		Flags: ioctl.PeerHasPublicKey |
			ioctl.PeerHasPresharedKey |
			ioctl.PeerHasEndpoint |
			ioctl.PeerHasPersistentKeepalive,
	}
	peer.Endpoint.SetIP(net.IPv4(192, 0, 2, 1), 51820)

	var b ioctl.ConfigBuilder
	b.AppendInterface(interfaze)
	b.AppendPeer(peer)
	got, _ := b.Interface()
	device := parseConfiguration("wg0", got)

	if !device.HasPrivateKey || !device.HasPublicKey || !device.HasListenPort {
		t.Fatalf("unexpected device field presence: %#v", device)
	}
	if device.HasFirewallMark {
		t.Fatal("WireGuardNT must not report firewall mark presence")
	}
	if len(device.Peers) != 1 {
		t.Fatalf("unexpected peer count: %d", len(device.Peers))
	}
	p := device.Peers[0]
	if !p.HasPresharedKey || !p.HasEndpoint || !p.HasPersistentKeepaliveInterval {
		t.Fatalf("unexpected peer field presence: %#v", p)
	}
}

func TestConfigurationAllowedIPOperations(t *testing.T) {
	_, legacy, _ := net.ParseCIDR("10.0.0.0/24")
	_, remove, _ := net.ParseCIDR("10.0.1.0/24")
	_, set, _ := net.ParseCIDR("2001:db8::/64")
	_, add, _ := net.ParseCIDR("10.0.2.0/24")
	cfg := wgtypes.Config{Peers: []wgtypes.PeerConfig{{
		AllowedIPs: []net.IPNet{*legacy},
		AllowedIPOperations: []wgtypes.AllowedIPConfig{
			{IPNet: *remove, Operation: wgtypes.AllowedIPRemove},
			{IPNet: *set, Operation: wgtypes.AllowedIPSet},
			{IPNet: *add, Operation: wgtypes.AllowedIPAdd},
		},
	}}}

	interfaze, size := buildConfiguration(cfg)
	wantSize := uint32(unsafe.Sizeof(ioctl.Interface{}) + unsafe.Sizeof(ioctl.Peer{}) + 4*unsafe.Sizeof(ioctl.AllowedIP{}))
	if size != wantSize {
		t.Fatalf("unexpected configuration size: got %d, want %d", size, wantSize)
	}
	peer := interfaze.FirstPeer()
	if got, want := peer.AllowedIPsCount, uint32(4); got != want {
		t.Fatalf("unexpected allowed IP count: got %d, want %d", got, want)
	}

	want := []struct {
		ip    net.IP
		cidr  uint8
		flags ioctl.AllowedIPFlag
	}{
		{legacy.IP, 24, 0},
		{remove.IP, 24, ioctl.AllowedIPRemove},
		{set.IP, 64, 0},
		{add.IP, 24, 0},
	}
	a := peer.FirstAllowedIP()
	for i, w := range want {
		if i != 0 {
			a = a.NextAllowedIP()
		}
		bits := 16
		if a.AddressFamily == windows.AF_INET {
			bits = 4
		}
		if !net.IP(a.Address[:bits]).Equal(w.ip) || a.Cidr != w.cidr || a.Flags != w.flags {
			t.Fatalf("allowed IP %d: got address %v/%d flags %d, want %v/%d flags %d", i, net.IP(a.Address[:bits]), a.Cidr, a.Flags, w.ip, w.cidr, w.flags)
		}
	}
}
