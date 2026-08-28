# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目范围

`wgctrl` 是一个 Go 库，用于查询和配置**已有的** WireGuard 设备。创建接口和分配 IP 地址不属于本仓库的职责范围。

本模块要求 Go 1.20，模块路径为 `golang.zx2c4.com/wireguard/wgctrl`。

## 开发命令

```bash
# 构建所有包及正式命令 cmd/wg、cmd/wgd
go build ./...

# 运行常规测试套件；这也是 Linux CI 使用的测试命令
go test -race ./...

# 在不支持 race detector 的平台运行测试
go test ./...

# 运行单个包或单个测试
go test ./internal/wguser
go test ./internal/wguser -run '^TestClientConfigureDeviceOK$'
go test . -run '^TestClientDevice$/first_not_found$'

# 格式化改动过的 Go 文件
gofmt -w path/to/file.go

# 执行必需的静态检查
go vet ./...
staticcheck ./...
```

需要时，可通过以下命令安装 CI 使用的静态分析工具：

```bash
go install honnef.co/go/tools/cmd/staticcheck@HEAD
```

集成测试具有破坏性：它会重置并重新配置检测到的所有 WireGuard 设备。只能在已经准备好内核和 userspace 接口的隔离测试环境中运行：

```bash
go test -c -race .
sudo env WGCTRL_INTEGRATION=yesreallydoit ./wgctrl.test -test.v -test.run TestIntegration
```

`.cibuild.sh` 用于配置 CI 主机并安装 `wireguard-go`。该脚本会克隆软件、创建接口并使用提权操作，不应作为常规的本地初始化脚本使用。

进行平台兼容性检查时，应指定目标 OS 和架构进行构建，例如 `GOOS=openbsd GOARCH=386 go build ./...`。FreeBSD 原生后端使用 cgo，必须在 FreeBSD 上通过 `CGO_ENABLED=1` 构建；OpenBSD CI 不支持 race detector。

## 架构

### 公共 API 与后端聚合

`client.go` 是公共 API 门面。`Client` 持有一个有序的 `internal/wginternal.Client` 实现列表，其统一接口包括 `Devices`、`Device`、`ConfigureDevice` 和 `Close`。

带有 build tag 的 `os_*.go` 文件负责为各平台构建后端列表。原生内核后端排在 userspace 后端之前。仅当当前后端返回与 `os.ErrNotExist` 兼容的错误时，`Device` 和 `ConfigureDevice` 才会继续尝试下一个后端；其他错误会立即终止分发。`Devices` 会直接合并各后端的结果，不执行去重。

各平台后端如下：

- `internal/wglinux`：使用 Linux generic netlink。设备枚举先通过 rtnetlink 筛选接口，再解码 WireGuard generic-netlink 消息。大型配置会拆分为多个批次；修改批处理逻辑时，必须保持首批 `ReplacePeers` 以及每个 peer 的 `ReplaceAllowedIPs` 语义。
- `internal/wguser`：实现跨平台 userspace 配置协议。它会发现 `/var/run/wireguard` 下的 UNIX socket 或受保护的 Windows named pipe，并交换以换行符分隔的 `get=1`、`set=1` 键值消息。
- `internal/wgwindows`：负责发现 WireGuardNT 适配器，并通过 `DeviceIoControl` 读写配置缓冲区。
- `internal/wgfreebsd`：使用 FreeBSD ioctl 和 nvlist 序列化；该后端依赖 cgo。
- `internal/wgopenbsd`：使用 OpenBSD ioctl。该后端可读取设备，但 `ConfigureDevice` 会返回内部只读错误标记。

### 共享模型与配置语义

`wgtypes` 是公共的、与后端无关的数据模型。`Device` 和 `Peer` 表示读取到的状态，`Config` 和 `PeerConfig` 表示配置变更。

配置类型中的指针字段是有意设计的：`nil` 表示“不修改”，指向零值的非 `nil` 指针通常表示“清除此设置”。替换和删除相关的布尔标记也会直接映射到协议行为。所有后端编码器都必须一致地保留这些区别。

所有后端都会将设备不存在或接口不是 WireGuard 设备的情况归一化为 `os.ErrNotExist`，以便公共 API 门面继续尝试其他后端。除非调用方需要底层权限错误或系统错误，否则不要向外暴露平台特定的传输错误。

### 测试

大部分协议解析和编码测试位于对应的 internal 包中，通过注入传输函数、系统调用函数和测试数据运行。`internal/wgtest` 提供共享的密钥与地址构造函数。仓库根目录下的单元测试验证多后端分发逻辑；根目录下的集成测试会操作检测到的真实设备。由于集成测试会修改设备状态，必须通过 `WGCTRL_INTEGRATION=yesreallydoit` 显式启用。

平台文件名和 build tag 是架构的一部分。修改后端后，应在目标 OS 上进行测试；如果无法测试，至少应针对目标 OS 编译。对于内核结构布局不同的平台，还需要覆盖相关的 32 位架构。
