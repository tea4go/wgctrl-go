// Package wgconf 为 wg 命令行工具与 wgd 守护进程提供共享的
// WireGuard 设备配置应用逻辑：将 wgtypes.Config 应用到设备，
// 并持久化对等节点的用户态名称元数据。
package wgconf
