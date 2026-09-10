package wgquick

import (
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Save 读取设备当前的加密配置，与配置文件中已有的网络字段合并后写回 path。
// 用于 `wg-quick save`：把运行中设备的状态固化到配置文件。
//
// 若配置文件尚不存在，则仅写入设备的加密配置。
func Save(path string) error {
	var cfg *Config
	f, err := os.Open(path)
	switch {
	case err == nil:
		cfg, err = Parse(f)
		f.Close()
		if err != nil {
			return err
		}
	case os.IsNotExist(err):
		cfg = &Config{}
	default:
		return err
	}
	cfg.Name = interfaceNameFromPath(path)

	client, err := wgctrl.New()
	if err != nil {
		return err
	}
	defer client.Close()

	dev, err := client.Device(cfg.Name)
	if err != nil {
		return err
	}
	applyDevice(cfg, dev)

	return writeFileAtomic(path, cfg)
}

// interfaceNameFromPath 从配置文件路径推导接口名（去掉 .conf 后缀）。
func interfaceNameFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// applyDevice 用设备当前状态覆盖配置中的加密字段，保留网络字段。
//
// 内核 netlink dump 会把「未设置」的可选字段带成零值（如 FwMark=0、
// 零预共享密钥、keepalive=0），因此按值判断：只有非零才写回。
// 例外是 PrivateKey——私钥未设置时内核会省略该属性，HasPrivateKey 是可靠的。
func applyDevice(cfg *Config, dev *wgtypes.Device) {
	cfg.PrivateKey = nil
	cfg.ListenPort = nil
	cfg.FirewallMark = nil

	if dev.HasPrivateKey {
		k := dev.PrivateKey
		cfg.PrivateKey = &k
	}
	if dev.ListenPort != 0 {
		p := dev.ListenPort
		cfg.ListenPort = &p
	}
	if dev.FirewallMark != 0 {
		m := dev.FirewallMark
		cfg.FirewallMark = &m
	}
	cfg.Peers = peersFromDevice(dev)
}

func peersFromDevice(dev *wgtypes.Device) []PeerConfig {
	peers := make([]PeerConfig, 0, len(dev.Peers))
	for _, p := range dev.Peers {
		pc := PeerConfig{
			PublicKey:  p.PublicKey,
			AllowedIPs: append([]net.IPNet(nil), p.AllowedIPs...),
		}
		if p.PresharedKey != (wgtypes.Key{}) {
			k := p.PresharedKey
			pc.PresharedKey = &k
		}
		if p.Endpoint != nil {
			ep := *p.Endpoint
			pc.Endpoint = &ep
		}
		if p.PersistentKeepaliveInterval != 0 {
			d := p.PersistentKeepaliveInterval
			pc.PersistentKeepalive = &d
		}
		peers = append(peers, pc)
	}
	return peers
}

// writeFileAtomic 将配置写回 path，先写临时文件再原子重命名。
func writeFileAtomic(path string, cfg *Config) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	encErr := Encode(f, cfg)
	closeErr := f.Close()
	if encErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if encErr != nil {
			return encErr
		}
		return closeErr
	}
	return os.Rename(tmp, path)
}
