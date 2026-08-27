package wgmeta

import (
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestStorePersistsNames(t *testing.T) {
	path := t.TempDir()
	store := New(path)
	key := wgtypes.Key{1}
	if err := store.Update("wg0", func(names map[wgtypes.Key]string) {
		names[key] = "branch-office"
	}); err != nil {
		t.Fatal(err)
	}

	names, err := New(path).Names("wg0")
	if err != nil {
		t.Fatal(err)
	}
	if names[key] != "branch-office" {
		t.Fatalf("name = %q", names[key])
	}
	other, err := New(path).Names("wg1")
	if err != nil || len(other) != 0 {
		t.Fatalf("other interface names=%v err=%v", other, err)
	}
}

func TestStoreDeletesEmptyInterface(t *testing.T) {
	path := t.TempDir()
	store := New(path)
	key := wgtypes.Key{1}
	if err := store.Update("wg0", func(names map[wgtypes.Key]string) { names[key] = "name" }); err != nil {
		t.Fatal(err)
	}
	if err := store.Update("wg0", func(names map[wgtypes.Key]string) { delete(names, key) }); err != nil {
		t.Fatal(err)
	}
	if names, err := store.Names("wg0"); err != nil || len(names) != 0 {
		t.Fatalf("names=%v err=%v", names, err)
	}
}
