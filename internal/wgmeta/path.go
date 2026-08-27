package wgmeta

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultPath 返回原生 WireGuard 配置文件所在的默认目录，
// 本目录下每个 interface 对应一对配置文件，例如：
//
//	Windows: wgtun0.conf.dpapi + wgtun0.names.json
//	Linux:   wg0.conf         + wg0.names.json
//	macOS:   utun.conf        + utun.names.json   (brew wireguard-tools)
//
// 可通过环境变量 WGCTRL_PEER_METADATA_DIR 覆盖默认目录。
func DefaultPath() string {
	if value := os.Getenv("WGCTRL_PEER_METADATA_DIR"); value != "" {
		return value
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("ProgramFiles"), "WireGuard", "Data", "Configurations")
	case "darwin":
		// Homebrew wireguard-tools：Apple Silicon → /opt/homebrew/etc/wireguard；
		// Intel Mac → /usr/local/etc/wireguard。优先返回实际存在的目录。
		if _, err := os.Stat("/opt/homebrew/etc/wireguard"); err == nil {
			return "/opt/homebrew/etc/wireguard"
		}
		return "/usr/local/etc/wireguard"
	default:
		return "/etc/wireguard"
	}
}

// DefaultFileFor 返回指定 interface 名称对应的默认 metadata 文件
// 的绝对路径，与该 interface 的原生 WireGuard 配置文件位于同一目录。
//
//	Windows: {Configurations}\{iface}.names.json
//	Linux:   /etc/wireguard/{iface}.names.json
//	macOS:   /opt/homebrew(或/usr/local)/etc/wireguard/{iface}.names.json
func DefaultFileFor(iface string) string {
	return filepath.Join(DefaultPath(), metadataFileNameFor(iface))
}

// ResolveFile 根据用户传入的 basePath 以及 interface 名称，
// 解析出最终应当读写的 metadata 文件路径。
//
// 规则：
//  1. basePath 为空 → 使用 DefaultFileFor(iface)。
//  2. basePath 是目录（或不存在且最后一段不带 .json 扩展名）
//     → 拼接 {basePath}/{iface}.names.json。
//  3. 否则 basePath 视为显式指定的具体 JSON 文件路径，直接返回。
//     （文件内容始终为扁平结构：{version:1, names:{公钥→友好名}}）
func ResolveFile(basePath string, iface string) string {
	base := strings.TrimSpace(basePath)
	if base == "" {
		return DefaultFileFor(iface)
	}
	if isLikelyDir(base) {
		return filepath.Join(base, metadataFileNameFor(iface))
	}
	return base
}

// metadataFileNameFor 返回 metadata 文件名（不含目录）。
// 与原生 {iface}.conf / {iface}.conf.dpapi 并列：{iface}.names.json
func metadataFileNameFor(iface string) string {
	name := strings.TrimSpace(iface)
	if name == "" {
		name = "unknown"
	}
	return name + ".names.json"
}

// isLikelyDir 判断给定路径是否"看起来像目录"：
//   - 路径存在且确实是目录 → true
//   - 路径不存在且最后一段扩展名不是 .json → true
//   - 否则 false（显式指定的 .json 文件）
func isLikelyDir(path string) bool {
	if info, err := os.Stat(path); err == nil {
		return info.IsDir()
	}
	return !strings.EqualFold(filepath.Ext(path), ".json")
}
