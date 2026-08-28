# `wg` CLI 完整兼容设计

## 目标

在 `wgctrl-go` 中实现一个名为 `wg`（Windows 为 `wg.exe`）的正式命令行程序，以 `C:\MyWork\GitCode\wireguard-tools` 当前版本的 `wg` 命令为兼容基准。

兼容范围包括子命令、参数、配置结果、标准输入输出、主要错误行为及退出状态。目标平台为 Linux、macOS 和 Windows。

本设计不包含 `wg-quick`，不负责创建或删除网络接口，也不管理 IP 地址、路由、DNS、防火墙和 hooks。

## 实现原则

- 设备访问继续通过 `wgctrl.Client`，不复制 Linux netlink、Windows ioctl 或 userspace IPC 实现。
- CLI 行为以 `wireguard-tools` 源码为准，不额外增强语义，也不以读取后整体替换等方式弱化底层已有能力。
- Linux、macOS 和 Windows 共用命令解析、配置解析与输出逻辑；平台差异保留在现有后端中。
- macOS 使用现有 userspace WireGuard socket 后端。
- 机器可读输出追求字节级兼容，pretty 输出对齐字段、排序、隐藏、单位和终端颜色规则。

## 组件结构

用正式的 `cmd/wg` 替代现有测试工具 `cmd/wgctrl`：

```text
cmd/wg/
├── main.go          # 进程入口和退出码
├── command.go       # 顶层命令分发、help、version
├── show.go          # show 和字段查询
├── showconf.go      # showconf
├── set.go           # set 参数解析
├── setconf.go       # setconf、addconf、syncconf
├── key.go           # genkey、genpsk、pubkey
└── *_test.go

internal/wgconfig/
├── parse.go         # wg(8) 配置文件解析
├── encode.go        # showconf 配置序列化
├── sync.go          # syncconf peer 差异计算
└── *_test.go

internal/wgcli/
├── format.go        # pretty、字段查询和 dump 输出
├── terminal.go      # TTY 检测与颜色控制
└── *_test.go
```

职责边界：

- `cmd/wg` 只处理参数、stdin、stdout、stderr 和退出状态。
- `internal/wgconfig` 只处理配置语义，不访问设备。
- `internal/wgcli` 只格式化已有设备数据。
- 所有设备读取和修改均通过 `wgctrl.Client`。
- 不保留两套独立 CLI 行为；旧 `cmd/wgctrl` 停止构建或移除。

## 命令兼容范围

无参数执行 `wg` 等同于 `wg show`。

支持以下顶层命令：

```text
wg show
wg showconf <interface>
wg set <interface> ...
wg setconf <interface> <configuration filename>
wg addconf <interface> <configuration filename>
wg syncconf <interface> <configuration filename>
wg genkey
wg genpsk
wg pubkey
wg help | -h | --help
wg version | -v | --version
```

### `show`

兼容：

```text
wg show { <interface> | all | interfaces }
    [public-key | private-key | listen-port | fwmark |
     peers | preshared-keys | endpoints | allowed-ips |
     latest-handshakes | transfer | persistent-keepalive | dump]
```

行为要求：

- `all` 查询所有设备，机器可读输出带接口名称列。
- `interfaces` 使用空格分隔接口名。
- `dump` 使用制表符分隔字段。
- 未设置的值按原版输出为 `(none)` 或 `off`。
- pretty 输出按最近握手时间倒序排列 peer；无握手的 peer 排在后面。
- private key 和 preshared key 默认显示 `(hidden)`。
- `WG_HIDE_KEYS=never` 时显示真实密钥。
- 流量单位使用 `B`、`KiB`、`MiB`、`GiB`、`TiB`。
- pretty 握手时间使用相对时间，机器可读格式使用 Unix 秒。
- 颜色仅在原版认为合适的终端环境中启用，管道和重定向输出不带颜色。

### `set`

兼容：

```text
wg set <interface>
    [listen-port <port>]
    [fwmark <mark>]
    [private-key <file>]
    [peer <public-key>
        [remove]
        [preshared-key <file>]
        [endpoint <host>:<port>]
        [persistent-keepalive <seconds|off>]
        [allowed-ips [+|-]<CIDR>[,...]]
    ]...
```

行为要求：

- 空 key 文件清除 private key 或 preshared key；支持平台对应的空设备文件用法。
- endpoint 支持 IPv4、`[IPv6]:port` 和域名解析。
- `WG_ENDPOINT_RESOLUTION_RETRIES` 控制临时 DNS 失败重试。
- `listen-port 0`、`fwmark off`、`persistent-keepalive off` 清除相应设置。
- `allowed-ips CIDR` 默认替换该 peer 的 AllowedIPs。
- `allowed-ips +CIDR` 和 `allowed-ips -CIDR` 使用 `wireguard-tools` 相同的底层增量标记和后端语义，不通过读取当前状态后整体替换来模拟。
- 参数错误写入 stderr，并返回非零退出状态。

## 配置文件

配置文件只接受 `wg(8)` 字段：

```ini
[Interface]
PrivateKey = ...
ListenPort = 51820
FwMark = 0xca6c

[Peer]
PublicKey = ...
PresharedKey = ...
Endpoint = example.com:51820
AllowedIPs = 10.0.0.0/24, 2001:db8::/64
PersistentKeepalive = 25
```

不接受以下 `wg-quick` 扩展字段：

- `Address`
- `DNS`
- `MTU`
- `Table`
- `PreUp`、`PostUp`
- `PreDown`、`PostDown`
- `SaveConfig`

遇到未知字段或非法值时返回配置解析错误，不静默忽略。

### `setconf`

- 替换全部 peer。
- 配置文件中的设备字段按文件值设置。
- 原版在非 append 模式下将缺失的 private key、listen port 和 fwmark 视为清除操作；Go 版保持相同语义。
- 每个 peer 的 AllowedIPs 默认替换。
- 文件中不存在的现有 peer 被删除。

### `addconf`

- 不替换全部 peer。
- 保留文件中未涉及的现有 peer。
- 文件中的 peer 被新增或更新。
- AllowedIPs 遵循配置解析器的原版语义：普通 CIDR 默认替换，`+CIDR` 追加，`-CIDR` 删除。

### `syncconf`

- 先读取当前设备，用于确定目标配置中不存在的 peer。
- 将这些 peer 标记为删除。
- 不设置全量替换 peer 标记，以避免不必要地重建未变化 peer。
- 对原版会补充清零的字段保持一致，包括配置文件缺失但运行时存在的 preshared key 和 persistent keepalive。
- 最终生成的变更标记与当前 `wireguard-tools/src/setconf.c` 的 `sync_conf` 语义一致。

## 数据模型扩展

现有读取模型无法可靠区分字段未配置和字段为零值。为正确实现 `show`、`showconf` 和 `dump`，在公共状态模型中加入存在性信息：

```go
type Device struct {
    // 现有字段
    HasPrivateKey   bool
    HasPublicKey    bool
    HasListenPort   bool
    HasFirewallMark bool
}

type Peer struct {
    // 现有字段
    HasPresharedKey                bool
    HasEndpoint                    bool
    HasPersistentKeepaliveInterval bool
}
```

各后端依据协议 attribute、flag 或键值是否出现填充这些字段。Linux、Windows 和 userspace 是验收重点；FreeBSD 和 OpenBSD 同步填充，避免公共模型在既有平台退化。

向公共结构体增加字段保持源码兼容，但可能影响比较完整结构体的调用方；仓库测试相应更新。

## AllowedIP 增量操作模型

扩展配置模型，使每条 AllowedIP 能表达与 `wireguard-tools` 相同的操作标记，而不是只有 peer 级 `ReplaceAllowedIPs`：

```go
type AllowedIPOperation uint8

const (
    AllowedIPSet AllowedIPOperation = iota
    AllowedIPAdd
    AllowedIPRemove
)

type AllowedIPConfig struct {
    IPNet     net.IPNet
    Operation AllowedIPOperation
}
```

具体公开 API 命名可在实现计划中按现有风格收敛，但必须满足：

- 普通 AllowedIP 与 `ReplaceAllowedIPs` 组合表达原版替换语义。
- `AllowedIPAdd` 映射原版无删除标记的增量项。
- `AllowedIPRemove` 映射 `WGALLOWEDIP_REMOVE_ME` 或平台等价标记。
- Linux、Windows 和 userspace 后端按照 `wireguard-tools` 对应后端的真实能力编码。
- 后端不支持某项协议能力时返回明确错误，不以非原子读改写模拟。

## 密钥命令

- `genkey` 使用安全随机源生成并 clamp private key，以 Base64 加换行输出。
- `genpsk` 使用安全随机源生成 preshared key，以 Base64 加换行输出。
- `pubkey` 从 stdin 读取一个 private key，验证格式和尾随内容，输出对应 public key。
- 空输入、非法 Base64、错误长度及非空白尾随字符均按原版失败。
- 密钥输出和临时文件处理遵循原版的权限与隐藏行为。

## 错误与退出状态

- 参数数量或名称错误：打印对应 usage 或 `Invalid argument`，返回非零状态。
- 配置文件打不开：stderr 输出文件错误，返回非零状态。
- 配置解析失败：stderr 输出具体原因及配置解析错误，返回非零状态。
- 设备不存在或不可访问：stderr 输出接口访问错误，返回非零状态。
- 单个设备查询失败时，`show all` 的继续或终止行为与原版一致。
- 成功命令返回 0。
- CLI 层集中管理退出状态，深层解析和格式化代码返回结构化错误，不直接结束进程。

## 兼容性测试

### 单元测试

- 参数解析覆盖所有子命令、合法输入、边界值和错误输入。
- 配置解析覆盖大小写、空白、注释、重复 section、缺失 public key、未知字段和非法值。
- AllowedIP 测试验证普通、`+`、`-` 三种操作产生正确标记。
- `syncconf` 测试覆盖 peer 新增、删除、保留及字段清零。
- 格式化测试覆盖 pretty、所有字段查询、dump 和 showconf。
- 密钥测试覆盖生成格式、pubkey 推导和全部错误输入。

### 差分测试

以 `C:\MyWork\GitCode\wireguard-tools` 当前 C 实现为 oracle：

- 对相同参数和配置输入比较解析结果及操作标记。
- 对固定设备状态比较 stdout、stderr 和退出状态。
- 机器可读输出要求字节级一致。
- pretty 输出比较字段、排序、隐藏、单位和颜色条件。
- 覆盖 `WG_HIDE_KEYS`、`WG_ENDPOINT_RESOLUTION_RETRIES`、TTY、管道和重定向场景。

### 集成测试

- Linux：在专用临时 WireGuard 接口上分别运行 C 版和 Go 版命令，比较最终状态。
- Windows：通过专用 WireGuardNT 测试适配器验证读取、配置和增量 AllowedIPs。
- macOS：通过专用 userspace WireGuard socket 验证读取与配置。
- 测试只操作专用测试接口；不得操作已有生产接口。
- 运行前记录测试接口状态，测试结束后清理或恢复。

## 验收标准

- Linux、macOS 和 Windows 均能构建名为 `wg` 的程序。
- 支持当前 `wireguard-tools` 的全部 `wg` 顶层子命令及参数。
- 对相同输入产生相同的 WireGuard 配置结果和操作语义。
- `+CIDR/-CIDR` 使用与原版相同的后端增量能力。
- 机器可读输出字节级兼容。
- pretty 输出的字段、排序、隐藏规则、单位和颜色条件兼容。
- stdin、stdout、stderr 和退出状态与原版主要行为兼容。
- 不包含任何 `wg-quick` 功能。
