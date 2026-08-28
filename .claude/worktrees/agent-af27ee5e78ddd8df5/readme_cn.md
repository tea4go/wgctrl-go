# wgctrl [![Test Status](https://github.com/WireGuard/wgctrl-go/workflows/Linux%20Test/badge.svg)](https://github.com/WireGuard/wgctrl-go/actions) [![Go Reference](https://pkg.go.dev/badge/golang.zx2c4.com/wireguard/wgctrl.svg)](https://pkg.go.dev/golang.zx2c4.com/wireguard/wgctrl) [![Go Report Card](https://goreportcard.com/badge/golang.zx2c4.com/wireguard/wgctrl)](https://goreportcard.com/report/golang.zx2c4.com/wireguard/wgctrl)


`wgctrl` 是一个用于在多个平台上控制 WireGuard 设备的 Go 包。

关于 WireGuard 的更多信息，请参见 <https://www.wireguard.com/>。

采用 MIT 许可证。

## 概述

`wgctrl` 可以控制多种类型的 WireGuard 设备，包括：

- 内核模块设备
  - Linux：通过通用 netlink 接口
  - FreeBSD：通过 ioctl 接口
  - OpenBSD：通过 ioctl 接口（只读）
  - Windows：通过 ioctl 接口
- 用户态设备（通过用户态配置协议）

随着新的操作系统加入对内核 WireGuard 实现的支持，
本包也应当随之扩展，以支持这些原生实现。

如果您了解到这方面的任何进展，请
[提交 issue](https://github.com/WireGuard/wgctrl-go/issues/new)。

本包实现了 WireGuard 配置协议的相关操作，可用于配置
已有的 WireGuard 设备。而诸如创建 WireGuard 设备、
为设备配置 IP 地址等操作，不属于本包的范围。
