package wgconf

import (
	"fmt"

	"golang.zx2c4.com/wireguard/wgctrl/internal/wgmeta"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Client 是应用配置所需的最小 WireGuard 设备控制接口。
type Client interface {
	Device(string) (*wgtypes.Device, error)
	ConfigureDevice(string, wgtypes.Config) error
}

// Apply 将 cfg 应用到 interfaceName，并把其中的对等节点名称
// 持久化到 metadataPath 指定的元数据文件中。
//
// WireGuard 原生协议不保存对等节点名称，因此名称被剥离后单独
// 交由 wgmeta 存储；配置生效后按设备实际状态更新元数据。
func Apply(c Client, interfaceName string, cfg wgtypes.Config, metadataPath string) error {
	native := cfg
	native.Peers = append([]wgtypes.PeerConfig(nil), cfg.Peers...)
	for i := range native.Peers {
		native.Peers[i].Name = nil
	}
	if err := c.ConfigureDevice(interfaceName, native); err != nil {
		return err
	}
	d, err := c.Device(interfaceName)
	if err != nil {
		return fmt.Errorf("WireGuard 配置已生效，但读取节点名称状态失败: %w", err)
	}
	exists := make(map[wgtypes.Key]bool, len(d.Peers))
	for _, peer := range d.Peers {
		exists[peer.PublicKey] = true
	}
	store := wgmeta.New(metadataPath)
	if err := store.Update(interfaceName, func(names map[wgtypes.Key]string) {
		if cfg.ReplacePeers {
			for key := range names {
				delete(names, key)
			}
		}
		for _, peer := range cfg.Peers {
			if peer.Remove {
				delete(names, peer.PublicKey)
				continue
			}
			if !exists[peer.PublicKey] {
				continue
			}
			if peer.Name != nil {
				if *peer.Name == "" {
					delete(names, peer.PublicKey)
				} else {
					names[peer.PublicKey] = *peer.Name
				}
			}
		}
	}); err != nil {
		return fmt.Errorf("WireGuard 配置已生效，但保存节点名称失败: %w", err)
	}
	return nil
}

// Sync 读取设备的当前状态，将 cfg 中缺失的现有对等节点标记为
// 移除，然后应用配置。用于 syncconf 语义：目标配置中的对等节点
// 列表与设备完全一致。
func Sync(c Client, interfaceName string, cfg wgtypes.Config, metadataPath string) error {
	current, err := c.Device(interfaceName)
	if err != nil {
		return err
	}
	cfg.Peers = syncPeerConfigs(current, cfg.Peers)
	return Apply(c, interfaceName, cfg, metadataPath)
}

// AttachNames 从 metadataPath 读取对等节点名称元数据，
// 并将其附加到设备的各个对等节点。
func AttachNames(d *wgtypes.Device, metadataPath string) error {
	names, err := wgmeta.New(metadataPath).Names(d.Name)
	if err != nil {
		return err
	}
	for i := range d.Peers {
		if name := names[d.Peers[i].PublicKey]; name != "" {
			d.Peers[i].Name = &name
		}
	}
	return nil
}

func syncPeerConfigs(current *wgtypes.Device, desired []wgtypes.PeerConfig) []wgtypes.PeerConfig {
	wanted := make(map[wgtypes.Key]bool, len(desired))
	out := append([]wgtypes.PeerConfig(nil), desired...)
	for _, peer := range desired {
		wanted[peer.PublicKey] = true
	}
	for _, peer := range current.Peers {
		if !wanted[peer.PublicKey] {
			out = append(out, wgtypes.PeerConfig{PublicKey: peer.PublicKey, Remove: true})
		}
	}
	return out
}
