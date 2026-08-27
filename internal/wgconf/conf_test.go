package wgconf

import (
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/internal/wgmeta"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type fakeClient struct {
	device     *wgtypes.Device
	configured wgtypes.Config
	err        error
}

func (c *fakeClient) Device(string) (*wgtypes.Device, error) { return c.device, c.err }
func (c *fakeClient) ConfigureDevice(_ string, cfg wgtypes.Config) error {
	c.configured = cfg
	return c.err
}

func TestApplyStripsAndPersistsName(t *testing.T) {
	name := "branch-office"
	key := wgtypes.Key{1}
	client := &fakeClient{device: &wgtypes.Device{Name: "wg0", Peers: []wgtypes.Peer{{PublicKey: key}}}}
	path := t.TempDir()
	cfg := wgtypes.Config{Peers: []wgtypes.PeerConfig{{Name: &name, PublicKey: key}}}
	if err := Apply(client, "wg0", cfg, path); err != nil {
		t.Fatal(err)
	}
	if client.configured.Peers[0].Name != nil {
		t.Fatalf("name passed to WireGuard backend: %#v", client.configured.Peers[0].Name)
	}
	names, err := wgmeta.New(path).Names("wg0")
	if err != nil || names[key] != name {
		t.Fatalf("persisted names=%v err=%v", names, err)
	}
}

func TestApplyClearsAndReplaces(t *testing.T) {
	path := t.TempDir()
	store := wgmeta.New(path)
	oldKey, keptKey := wgtypes.Key{1}, wgtypes.Key{2}
	if err := store.Update("wg0", func(names map[wgtypes.Key]string) {
		names[oldKey] = "old"
		names[keptKey] = "kept"
	}); err != nil {
		t.Fatal(err)
	}
	empty := ""
	client := &fakeClient{device: &wgtypes.Device{Name: "wg0", Peers: []wgtypes.Peer{{PublicKey: keptKey}}}}
	cfg := wgtypes.Config{ReplacePeers: true, Peers: []wgtypes.PeerConfig{{Name: &empty, PublicKey: keptKey}}}
	if err := Apply(client, "wg0", cfg, path); err != nil {
		t.Fatal(err)
	}
	names, err := store.Names("wg0")
	if err != nil || len(names) != 0 {
		t.Fatalf("names=%v err=%v", names, err)
	}
}

func TestSyncRemovesMissingPeers(t *testing.T) {
	path := t.TempDir()
	kept, removed := wgtypes.Key{1}, wgtypes.Key{2}
	client := &fakeClient{
		device: &wgtypes.Device{Name: "wg0", Peers: []wgtypes.Peer{{PublicKey: kept}, {PublicKey: removed}}},
	}
	cfg := wgtypes.Config{Peers: []wgtypes.PeerConfig{{PublicKey: kept}}}
	if err := Sync(client, "wg0", cfg, path); err != nil {
		t.Fatal(err)
	}
	got := client.configured.Peers
	if len(got) != 2 || !got[1].Remove || got[1].PublicKey != removed {
		t.Fatalf("sync peers=%#v", got)
	}
}

func TestAttachNamesUsesPeerKeyNotDeviceName(t *testing.T) {
	path := t.TempDir()
	key := wgtypes.Key{1}
	if err := wgmeta.New(path).Update("wg0", func(names map[wgtypes.Key]string) { names[key] = "node-a" }); err != nil {
		t.Fatal(err)
	}
	d := &wgtypes.Device{Name: "wg0", Peers: []wgtypes.Peer{{PublicKey: key}}}
	if err := AttachNames(d, path); err != nil {
		t.Fatal(err)
	}
	if d.Peers[0].Name == nil || *d.Peers[0].Name != "node-a" {
		t.Fatalf("peer name=%#v", d.Peers[0].Name)
	}
}
