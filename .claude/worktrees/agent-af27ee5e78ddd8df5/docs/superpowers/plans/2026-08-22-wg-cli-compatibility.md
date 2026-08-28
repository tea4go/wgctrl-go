# wg CLI 兼容实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 在 `wgctrl-go` 中实现跨 Linux、macOS 和 Windows 的正式 `cmd/wg`（Windows 为 `wg.exe`），兼容 `wireguard-tools` 当前 `wg` 命令的子命令、参数、配置语义、主要输出、错误和退出状态，不包含 `wg-quick`。

**架构：** CLI 通过现有 `wgctrl.Client` 访问设备；`internal/wgconfig` 负责 `wg(8)` 配置解析、序列化和 `syncconf` 差异计算；`internal/wgcli` 负责设备状态格式化及终端颜色。扩展 `wgtypes.PeerConfig`，让每条 AllowedIP 携带原版操作标记，并由后端直接编码原生增量能力，不以读回后整体替换模拟。

**技术栈：** Go 1.20、`wgctrl.Client`、`wgtypes`、Linux generic netlink、WireGuardNT ioctl、WireGuard userspace protocol、Go 标准库、现有 `golang.org/x/crypto`，以及 `C:\MyWork\GitCode\wireguard-tools` C 实现作为差分 oracle。

---

## 文件结构与职责

### 创建

- `cmd/wg/main.go`：进程入口，唯一调用 `os.Exit` 的位置。
- `cmd/wg/command.go`：顶层分发、无参数转 `show`、help、version、公共依赖注入。
- `cmd/wg/show.go`：`show` 和字段查询的参数处理及设备读取。
- `cmd/wg/showconf.go`：`showconf` 参数处理及设备读取。
- `cmd/wg/set.go`：`set` 参数、key 文件、endpoint 和 AllowedIPs 解析。
- `cmd/wg/setconf.go`：`setconf`、`addconf`、`syncconf` 的文件读取与调用编排。
- `cmd/wg/key.go`：`genkey`、`genpsk`、`pubkey`。
- `cmd/wg/*_test.go`：命令的 stdin/stdout/stderr、usage 和退出码测试。
- `internal/wgconfig/parse.go`：严格解析 `wg(8)` 配置，不接受 `wg-quick` 字段。
- `internal/wgconfig/encode.go`：生成 `showconf` 文本。
- `internal/wgconfig/sync.go`：计算 `syncconf` peer 删除及字段清零。
- `internal/wgconfig/*_test.go`：配置语义测试。
- `internal/wgcli/format.go`：pretty、字段查询、dump、排序、时间和流量格式化。
- `internal/wgcli/terminal.go`：TTY 检测、`WG_COLOR_MODE` 和 ANSI 输出控制。
- `internal/wgcli/*_test.go`：固定状态的字节级输出测试。
- `internal/wgcli/testdata/`：固定设备状态对应的 golden 输出。

### 修改

- `wgtypes/types.go`：增加状态字段存在性；保留现有 `AllowedIPs []net.IPNet`，并新增每条 AllowedIP 的操作模型。
- `wgtypes/errors.go`：仅增加后端无法表达某项配置能力时的明确错误。
- `internal/wglinux/{parse_linux.go,configure_linux.go,*_test.go}`：存在性和 `WGALLOWEDIP_F_REMOVE_ME`。
- `internal/wgwindows/client_windows.go`、`internal/wgwindows/internal/ioctl/configuration_windows.go`：WireGuardNT 存在性和 AllowedIP remove flag。
- `internal/wguser/{parse.go,configure.go,*_test.go}`：userspace 字段存在性和 `allowed_ip=-CIDR`。
- `internal/wgfreebsd/client_freebsd.go`：同步状态存在性和 nvlist AllowedIP flags。
- `internal/wgopenbsd/client_openbsd.go`：同步状态存在性；保持配置只读。
- `cmd/wgctrl/main.go`：最终移除或停止构建旧诊断程序，避免维护两套 CLI。
- `.github/workflows/*.yml`：构建、单元测试、静态检查和专用集成测试覆盖。

### Oracle（只读）

- `C:\MyWork\GitCode\wireguard-tools\src\wg.c`
- `C:\MyWork\GitCode\wireguard-tools\src\show.c`
- `C:\MyWork\GitCode\wireguard-tools\src\showconf.c`
- `C:\MyWork\GitCode\wireguard-tools\src\set.c`
- `C:\MyWork\GitCode\wireguard-tools\src\setconf.c`
- `C:\MyWork\GitCode\wireguard-tools\src\config.c`
- `C:\MyWork\GitCode\wireguard-tools\src\genkey.c`
- `C:\MyWork\GitCode\wireguard-tools\src\pubkey.c`
- `C:\MyWork\GitCode\wireguard-tools\src\terminal.c`
- `C:\MyWork\GitCode\wireguard-tools\src\ipc-*.h`

---

## 阶段 1：公共模型和平台协议能力

### 任务 1：建立可测试的 `cmd/wg` 入口

**文件：**
- 创建：`cmd/wg/main.go`
- 创建：`cmd/wg/command.go`
- 创建：`cmd/wg/command_test.go`

- [ ] **步骤 1：编写失败测试**

在 `command_test.go` 测试纯函数入口：

```go
func TestExecuteVersion(t *testing.T) {
    var out, errOut bytes.Buffer
    code := execute([]string{"version"}, strings.NewReader(""), &out, &errOut)
    if code != 0 {
        t.Fatalf("unexpected exit code: %d", code)
    }
    if want := "wireguard-tools v1.0.20260223 - https://git.zx2c4.com/wireguard-tools/\n"; out.String() != want {
        t.Fatalf("unexpected stdout: %q", out.String())
    }
    if errOut.Len() != 0 {
        t.Fatalf("unexpected stderr: %q", errOut.String())
    }
}
```

同时覆盖 `-v`、`--version`、`help`、`-h`、`--help`、非法子命令，以及无参数调用注入的 `show` handler。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./cmd/wg -run TestExecute -count=1`。

预期：因包或 `execute` 尚不存在而失败。

- [ ] **步骤 3：编写最少实现代码**

实现：

```go
func main() {
    os.Exit(execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
```

`execute` 只完成顶层分发和可注入 handler；深层函数返回错误或状态，不调用 `log.Fatal`、`panic` 或 `os.Exit`。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./cmd/wg -run TestExecute -count=1`。

预期：分发、help、version 和非法子命令测试通过，且不访问真实设备。

- [ ] **步骤 5：Commit**

```bash
git add cmd/wg
git commit -m "feat: add testable wg command entrypoint"
```

### 任务 2：扩展状态存在性和 AllowedIP 操作类型

**文件：**
- 修改：`wgtypes/types.go`
- 修改：`wgtypes/errors.go`
- 创建：`wgtypes/types_test.go`（若不存在）
- 修改：现有后端测试，使其同时覆盖旧 `AllowedIPs` 字段和新增操作字段

- [ ] **步骤 1：编写失败测试**

```go
func TestAllowedIPOperations(t *testing.T) {
    _, ipn, _ := net.ParseCIDR("10.0.0.0/24")
    got := []wgtypes.AllowedIPConfig{
        {IPNet: *ipn, Operation: wgtypes.AllowedIPSet},
        {IPNet: *ipn, Operation: wgtypes.AllowedIPAdd},
        {IPNet: *ipn, Operation: wgtypes.AllowedIPRemove},
    }
    if got[0].Operation == got[1].Operation || got[1].Operation == got[2].Operation {
        t.Fatal("operations are not distinct")
    }
}
```

另用结构体字面量断言 `Device.HasPrivateKey`、`HasPublicKey`、`HasListenPort`、`HasFirewallMark` 及 `Peer.HasPresharedKey`、`HasEndpoint`、`HasPersistentKeepaliveInterval` 可独立表达。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./wgtypes -run 'TestAllowedIPOperations|TestFieldPresence' -count=1`。

预期：类型和字段尚不存在，编译失败。

- [ ] **步骤 3：编写最少实现代码**

加入：

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

type PeerConfig struct {
    // 其他现有字段保持不变。
    ReplaceAllowedIPs  bool
    AllowedIPs         []net.IPNet
    AllowedIPOperations []AllowedIPConfig
}
```

保留现有 `PeerConfig.AllowedIPs []net.IPNet`，新增 `AllowedIPOperations []AllowedIPConfig`，继续用 `ReplaceAllowedIPs` 表达 peer 级替换。旧字段中的项目按既有语义编码；新字段中 `AllowedIPSet` 是普通项，`AllowedIPAdd` 是显式增量添加，`AllowedIPRemove` 是原生删除。CLI 只填充新字段以保留输入顺序；若库调用方同时填充两个字段，后端先编码旧字段，再编码新字段。这样现有源码继续编译，新增功能也不依赖读改写。

- [ ] **步骤 4：运行测试验证通过**

运行：

```bash
go test ./wgtypes ./internal/wgtest -count=1
go test ./... -run '^$' -count=1
```

预期：模型测试通过，所有包完成编译。

- [ ] **步骤 5：Commit**

```bash
git add wgtypes internal
git commit -m "feat: model field presence and allowed IP operations"
```

### 任务 3：Linux 后端保留字段和增量标记

**文件：**
- 修改：`internal/wglinux/parse_linux.go`
- 修改：`internal/wglinux/parse_linux_test.go`
- 修改：`internal/wglinux/configure_linux.go`
- 修改：`internal/wglinux/configure_linux_test.go`

- [ ] **步骤 1：编写失败测试**

给 netlink fixture 增加「attribute 出现但值为零」用例，断言相应 `Has*` 为 true；给 AllowedIP nested attribute 增加：

```go
{
    Type: unix.WGALLOWEDIP_A_FLAGS,
    Data: nlenc.Uint32Bytes(unix.WGALLOWEDIP_F_REMOVE_ME),
}
```

断言 `AllowedIPRemove` 产生此 attribute，并在超过 `ipBatchChunk` 时操作标记不丢失。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/wglinux -run 'TestLinuxClientDevicesOK|TestLinuxClientConfigureDevice' -count=1`。

预期：存在性字段仍为 false，或请求中缺少 AllowedIP remove flag。

- [ ] **步骤 3：编写最少实现代码**

在 `parseDeviceLoop` 和 `parsePeer` 看到对应 attribute 时设置 `Has*`。让 `encodeAllowedIPs` 依次编码旧 `AllowedIPs` 和新 `AllowedIPOperations`，为 `AllowedIPRemove` 写入 `WGALLOWEDIP_A_FLAGS`。更新 `shouldBatch` 和 `buildBatches`，按两组项目的总数分片，确保新字段的每个切片保留 `Operation`，并继续只在同一 peer 首个切片设置 `ReplaceAllowedIPs`。

- [ ] **步骤 4：运行测试验证通过**

运行：

```bash
go test ./internal/wglinux -count=1
go test ./... -count=1
```

预期：Linux 定向测试和全量单元测试通过。

- [ ] **步骤 5：Commit**

```bash
git add internal/wglinux
git commit -m "feat: preserve Linux WireGuard operation flags"
```

### 任务 4：Windows WireGuardNT 后端保留字段和增量标记

**文件：**
- 修改：`internal/wgwindows/internal/ioctl/configuration_windows.go`
- 修改：`internal/wgwindows/client_windows.go`
- 创建：`internal/wgwindows/configuration_windows_test.go`

- [ ] **步骤 1：编写失败测试**

将 ioctl buffer 的解析和构造提炼为同包 helper 后，使用内存 fixture 断言 interface/peer 的 `Has*` flags 映射；构造 `AllowedIPRemove`，断言序列化后的 `ioctl.AllowedIP.Flags` 为 `1 << 0`。

- [ ] **步骤 2：运行测试验证失败**

在 Windows 运行：`go test ./internal/wgwindows/... -run TestConfiguration -count=1`。

预期：缺少 AllowedIP flag 字段或 helper，测试失败。

- [ ] **步骤 3：编写最少实现代码**

使 Go 结构与 oracle `uapi/windows/wireguard.h` 对齐：

```go
type AllowedIPFlag uint32
const AllowedIPRemove AllowedIPFlag = 1 << 0
```

在读取时设置存在性，在配置 buffer 中映射 remove flag。不得修改 named pipe 所有者安全校验或设备发现逻辑。

- [ ] **步骤 4：运行测试验证通过**

Windows 原生运行：

```bash
go test ./internal/wgwindows/... -count=1
go test ./... -count=1
```

其他平台至少执行：`GOOS=windows GOARCH=amd64 go test ./internal/wgwindows/... -run '^$'`。

预期：原生测试通过；交叉编译通过。

- [ ] **步骤 5：Commit**

```bash
git add internal/wgwindows
git commit -m "feat: encode WireGuardNT allowed IP operations"
```

### 任务 5：userspace、FreeBSD 和 OpenBSD 后端同步语义

**文件：**
- 修改：`internal/wguser/parse.go`
- 修改：`internal/wguser/parse_test.go`
- 修改：`internal/wguser/configure.go`
- 修改：`internal/wguser/configure_test.go`
- 修改：`internal/wgfreebsd/client_freebsd.go`
- 修改：`internal/wgopenbsd/client_openbsd.go`
- 测试：对应平台现有测试文件

- [ ] **步骤 1：编写失败测试**

userspace parser 测试字段存在但为零的情况；配置 golden 直接按 oracle 协议断言：

```text
allowed_ip=10.0.0.0/24
allowed_ip=-10.0.1.0/24
```

显式增量添加在 userspace 协议中不输出 `+`，而是依靠 peer 不带 `replace_allowed_ips=true` 表达；删除项输出 `-`。FreeBSD fixture 断言 nvlist AllowedIP 的 `flags=1`；OpenBSD 测试继续断言配置返回 `wginternal.ErrReadOnly`。

- [ ] **步骤 2：运行测试验证失败**

运行：

```bash
go test ./internal/wguser -count=1
GOOS=openbsd GOARCH=amd64 go test ./internal/wgopenbsd -run '^$'
```

FreeBSD 测试必须在 FreeBSD 上用 `CGO_ENABLED=1 go test ./internal/wgfreebsd/...` 运行。

- [ ] **步骤 3：编写最少实现代码**

userspace 看到字段时设置 `Has*`；`AllowedIPRemove` 输出 `allowed_ip=-CIDR`，Set/Add 输出无前缀 CIDR，由 `ReplaceAllowedIPs` 决定替换或追加。FreeBSD 的 `unparseAllowedIP` 写入 `flags`；OpenBSD 只补读取存在性，不伪造配置支持。

- [ ] **步骤 4：运行测试验证通过**

运行：

```bash
go test ./internal/wguser -count=1
go test ./... -count=1
GOOS=darwin GOARCH=amd64 go build ./cmd/wg
GOOS=openbsd GOARCH=amd64 go build ./...
```

预期：userspace 和全量测试通过，macOS/OpenBSD 构建通过；FreeBSD 原生验证另列为平台验收项。

- [ ] **步骤 5：Commit**

```bash
git add internal/wguser internal/wgfreebsd internal/wgopenbsd
git commit -m "feat: preserve userspace and BSD WireGuard flags"
```

---

## 阶段 2：配置解析、序列化和同步语义

### 任务 6：实现严格的 `wg(8)` 配置解析器

**文件：**
- 创建：`internal/wgconfig/parse.go`
- 创建：`internal/wgconfig/parse_test.go`

- [ ] **步骤 1：编写失败测试**

表驱动覆盖：大小写、空白、`#` 注释、重复 `[Peer]`、缺失 peer public key、IPv4/IPv6、endpoint、hex fwmark、`off` keepalive、普通/`+`/`-` AllowedIPs、空 AllowedIPs、未知字段，以及 `Address`、`DNS`、`MTU`、`Table`、hooks、`SaveConfig` 全部拒绝。

核心断言：

```go
if got.Peers[0].AllowedIPs[1].Operation != wgtypes.AllowedIPAdd ||
   got.Peers[0].AllowedIPs[2].Operation != wgtypes.AllowedIPRemove {
    t.Fatalf("unexpected operations: %#v", got.Peers[0].AllowedIPs)
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/wgconfig -run TestParse -count=1`。

预期：包或 `Parse` 不存在。

- [ ] **步骤 3：编写最少实现代码**

定义 `Parse(r io.Reader, appendMode bool) (wgtypes.Config, error)`。非 append 模式初始化 `PrivateKey=&zeroKey`、`ListenPort=&zeroInt`、`FirewallMark=&zeroInt`、`ReplacePeers=true`；append 模式不设置缺失设备字段。每个 `[Peer]` 默认 `ReplaceAllowedIPs=true`，遇到任一 `+` 或 `-` 前缀时按 C oracle 清除此 peer 的替换标记。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./internal/wgconfig -run TestParse -count=1`。

预期：合法、边界和错误输入均通过，错误中包含导致失败的字段或值。

- [ ] **步骤 5：Commit**

```bash
git add internal/wgconfig/parse.go internal/wgconfig/parse_test.go
git commit -m "feat: parse wg configuration files"
```

### 任务 7：实现 `showconf` 序列化

**文件：**
- 创建：`internal/wgconfig/encode.go`
- 创建：`internal/wgconfig/encode_test.go`

- [ ] **步骤 1：编写失败测试**

构造固定 `wgtypes.Device`，覆盖存在/缺失的 private key、port、fwmark、PSK、AllowedIPs、IPv4/IPv6 endpoint 和 keepalive；golden 必须与 `showconf.c` 的空行和字段顺序一致。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/wgconfig -run TestEncode -count=1`。

预期：`Encode` 不存在。

- [ ] **步骤 3：编写最少实现代码**

实现 `Encode(w io.Writer, d *wgtypes.Device) error`；只依据 `Has*` 和原版零值规则输出字段，endpoint 使用数字地址，IPv6 加方括号。不要输出运行时统计、public key 之外的状态字段或任何 `wg-quick` 字段。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./internal/wgconfig -run TestEncode -count=1`。

预期：输出逐字节匹配 golden。

- [ ] **步骤 5：Commit**

```bash
git add internal/wgconfig/encode.go internal/wgconfig/encode_test.go
git commit -m "feat: encode wg showconf output"
```

### 任务 8：实现 `syncconf` 差异计算

**文件：**
- 创建：`internal/wgconfig/sync.go`
- 创建：`internal/wgconfig/sync_test.go`

- [ ] **步骤 1：编写失败测试**

覆盖：文件无 peer、运行时无 peer、文件新增、运行时多余 peer 删除、相同 peer 保留、运行时 PSK 存在而文件缺失时清零、运行时 keepalive 非零而文件缺失时清零，以及 `ReplacePeers` 被取消。

```go
func TestSyncClearsOmittedPeerFields(t *testing.T) {
    got := Sync(runtime, target)
    if got.ReplacePeers {
        t.Fatal("syncconf must not replace all peers")
    }
    if got.Peers[0].PresharedKey == nil || *got.Peers[0].PresharedKey != (wgtypes.Key{}) {
        t.Fatal("missing preshared key clear")
    }
}
```

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/wgconfig -run TestSync -count=1`。

预期：`Sync` 不存在。

- [ ] **步骤 3：编写最少实现代码**

按 public key 排序或建索引，复刻 `setconf.c:sync_conf`：仅为目标缺失的运行时 peer 生成 `Remove=true`；匹配 peer 只补 PSK/keepalive 清零；取消 `ReplacePeers`；不做未被 C 实现执行的通用最小 diff 优化。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./internal/wgconfig -count=1`。

预期：parse、encode、sync 全部通过。

- [ ] **步骤 5：Commit**

```bash
git add internal/wgconfig/sync.go internal/wgconfig/sync_test.go
git commit -m "feat: compute wg syncconf peer changes"
```

---

## 阶段 3：输出格式兼容

### 任务 9：实现 pretty、字段查询和 dump 格式化

**文件：**
- 创建：`internal/wgcli/format.go`
- 创建：`internal/wgcli/format_test.go`
- 创建：`internal/wgcli/testdata/*.golden`

- [ ] **步骤 1：编写失败测试**

用固定时间注入和固定设备状态覆盖：peer 按握手倒序、无握手排后；private/PSK 默认 `(hidden)`，`WG_HIDE_KEYS=never` 显示；`(none)`、`off`；B/KiB/MiB/GiB/TiB；未来时钟警告；所有机器字段；单接口和 `all` 时接口名前缀；dump 的制表符和换行。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/wgcli -run TestFormat -count=1`。

预期：包或 formatter 不存在。

- [ ] **步骤 3：编写最少实现代码**

定义明确入口，例如：

```go
func Pretty(w io.Writer, d *wgtypes.Device, opts Options) error
func Field(w io.Writer, d *wgtypes.Device, field string, withInterface bool) error
func Dump(w io.Writer, d *wgtypes.Device, withInterface bool) error
```

`Options` 只注入测试所需的当前时间、颜色和 hide-keys 环境结果；不要引入通用模板系统。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./internal/wgcli -run TestFormat -count=1`。

预期：所有输出与 golden 字节级一致。

- [ ] **步骤 5：Commit**

```bash
git add internal/wgcli/format.go internal/wgcli/format_test.go internal/wgcli/testdata
git commit -m "feat: format wg device output"
```

### 任务 10：实现终端颜色规则

**文件：**
- 创建：`internal/wgcli/terminal.go`
- 创建：`internal/wgcli/terminal_unix.go`
- 创建：`internal/wgcli/terminal_windows.go`
- 创建：`internal/wgcli/terminal_test.go`

- [ ] **步骤 1：编写失败测试**

覆盖 `WG_COLOR_MODE=always`、`never`、未设置且 stdout 为 TTY、未设置且为 pipe；断言非颜色模式无 ANSI escape。Windows 测试通过注入 TTY 判定，不依赖 CI 控制台。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./internal/wgcli -run TestColorMode -count=1`。

预期：颜色判定尚不存在。

- [ ] **步骤 3：编写最少实现代码**

按 `terminal.c` 优先级实现：环境变量 `always`/`never` 优先，否则依据 stdout TTY。平台文件只负责 TTY 查询；格式化层负责是否写 ANSI，不做字符串事后清理。

- [ ] **步骤 4：运行测试验证通过**

运行：

```bash
go test ./internal/wgcli -count=1
GOOS=windows GOARCH=amd64 go test ./internal/wgcli -run '^$'
GOOS=darwin GOARCH=amd64 go test ./internal/wgcli -run '^$'
```

预期：本机测试及 Windows/macOS 编译通过。

- [ ] **步骤 5：Commit**

```bash
git add internal/wgcli
git commit -m "feat: match wg terminal color behavior"
```

---

## 阶段 4：读取命令

### 任务 11：实现 `show` 和字段查询

**文件：**
- 创建：`cmd/wg/show.go`
- 创建：`cmd/wg/show_test.go`
- 修改：`cmd/wg/command.go`

- [ ] **步骤 1：编写失败测试**

注入假的 client 接口：

```go
type deviceClient interface {
    Devices() ([]*wgtypes.Device, error)
    Device(string) (*wgtypes.Device, error)
    ConfigureDevice(string, wgtypes.Config) error
    Close() error
}
```

测试 `show`、`show all`、`show interfaces`、单接口、全部字段、非法字段、参数过多、列表失败、单设备失败，以及 `show all` 某设备失败后继续行为与 `show.c` 一致。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./cmd/wg -run TestShow -count=1`。

预期：handler 不存在或输出不匹配。

- [ ] **步骤 3：编写最少实现代码**

仅在命令需要设备时创建 client；`interfaces` 输出空格分隔名称；`all` 的机器格式传 `withInterface=true`；pretty 多接口间只输出一个空行。设备枚举顺序保持后端返回顺序，不擅自排序。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./cmd/wg -run TestShow -count=1`。

预期：参数、输出、错误和退出码测试通过。

- [ ] **步骤 5：Commit**

```bash
git add cmd/wg/show.go cmd/wg/show_test.go cmd/wg/command.go
git commit -m "feat: implement wg show commands"
```

### 任务 12：实现 `showconf`

**文件：**
- 创建：`cmd/wg/showconf.go`
- 创建：`cmd/wg/showconf_test.go`
- 修改：`cmd/wg/command.go`

- [ ] **步骤 1：编写失败测试**

测试参数必须恰为一个接口、设备错误写 stderr、成功输出完全由 `wgconfig.Encode` 生成，并验证 private key 按原版显示真实值而不是受 `WG_HIDE_KEYS` 影响。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./cmd/wg -run TestShowconf -count=1`。

预期：命令尚未实现。

- [ ] **步骤 3：编写最少实现代码**

读取 `client.Device(name)`，调用 `wgconfig.Encode(out, d)`；usage 和错误前缀与 `showconf.c` 对齐。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./cmd/wg -run TestShowconf -count=1`。

预期：全部通过。

- [ ] **步骤 5：Commit**

```bash
git add cmd/wg/showconf.go cmd/wg/showconf_test.go cmd/wg/command.go
git commit -m "feat: implement wg showconf"
```

---

## 阶段 5：配置命令

### 任务 13：实现共享值解析和 endpoint DNS 重试

**文件：**
- 创建：`cmd/wg/value.go`
- 创建：`cmd/wg/value_test.go`

- [ ] **步骤 1：编写失败测试**

覆盖 port 0..65535、fwmark 十进制/十六进制/`off`、keepalive 0/`off`/1..65535、IPv4、`[IPv6]:port`、域名、无效 endpoint，以及 `WG_ENDPOINT_RESOLUTION_RETRIES` 的临时 DNS 错误重试次数。resolver 必须注入，测试不得访问公网 DNS。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./cmd/wg -run 'TestParse|TestResolveEndpoint' -count=1`。

预期：解析函数不存在。

- [ ] **步骤 3：编写最少实现代码**

按 `config.c` 的范围和错误文本实现；只对 `net.Error.Temporary()` 或 oracle 等价临时错误重试。环境变量非法时沿用原版默认行为，不增加未要求的指数退避。

- [ ] **步骤 4：运行测试验证通过**

运行：`go test ./cmd/wg -run 'TestParse|TestResolveEndpoint' -count=1`。

预期：边界和错误测试通过。

- [ ] **步骤 5：Commit**

```bash
git add cmd/wg/value.go cmd/wg/value_test.go
git commit -m "feat: parse wg command values and endpoints"
```

### 任务 14：实现 `set`

**文件：**
- 创建：`cmd/wg/set.go`
- 创建：`cmd/wg/set_test.go`
- 修改：`cmd/wg/command.go`

- [ ] **步骤 1：编写失败测试**

覆盖所有设备字段、多个 peer、remove、key 文件、空 key 文件、普通/`+`/`-` AllowedIPs、逗号列表、重复 AllowedIPs 参数、非法参数位置和缺失值。使用临时文件；Unix 的 `/dev/null` 和 Windows 的空文件分别做平台测试。

关键预期：普通 CIDR 设置 `ReplaceAllowedIPs=true`；任一前缀项使该 peer 的 `ReplaceAllowedIPs=false`；`-CIDR` 生成 `AllowedIPRemove`，`+CIDR` 生成 `AllowedIPAdd`。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./cmd/wg -run TestSet -count=1`。

预期：命令尚未实现。

- [ ] **步骤 3：编写最少实现代码**

从左到右构造单个 `wgtypes.Config`，只调用一次 `ConfigureDevice`。key 文件读取严格接受一个 Base64 key 或空内容；清除用非 nil 零 key。参数错误写入 stderr，返回 1。

- [ ] **步骤 4：运行测试验证通过**

运行：

```bash
go test ./cmd/wg -run TestSet -count=1
go test ./cmd/wg ./internal/wguser ./internal/wglinux -count=1
```

预期：set 及后端编码测试通过。

- [ ] **步骤 5：Commit**

```bash
git add cmd/wg/set.go cmd/wg/set_test.go cmd/wg/command.go
git commit -m "feat: implement wg set"
```

### 任务 15：实现 `setconf`、`addconf` 和 `syncconf`

**文件：**
- 创建：`cmd/wg/setconf.go`
- 创建：`cmd/wg/setconf_test.go`
- 修改：`cmd/wg/command.go`

- [ ] **步骤 1：编写失败测试**

测试 usage、文件打不开、解析错误、设备修改错误；`setconf` 调用 `Parse(..., false)`；`addconf` 调用 `Parse(..., true)`；`syncconf` 先读当前设备，再调用 `wgconfig.Sync`。测试 `-` 是否被当作普通文件名：以当前 C oracle 实际行为为准，不自行添加 stdin 扩展。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./cmd/wg -run 'TestSetconf|TestAddconf|TestSyncconf' -count=1`。

预期：命令尚未实现。

- [ ] **步骤 3：编写最少实现代码**

三个命令共享文件读取和错误映射，但保留各自模式。每个成功路径只调用一次 `ConfigureDevice`；`syncconf` 额外调用一次 `Device`。不得通过读取当前 AllowedIPs 后模拟 `+/-`。

- [ ] **步骤 4：运行测试验证通过**

运行：

```bash
go test ./cmd/wg -run 'TestSetconf|TestAddconf|TestSyncconf' -count=1
go test ./internal/wgconfig ./cmd/wg -count=1
```

预期：三个命令及配置层测试通过。

- [ ] **步骤 5：Commit**

```bash
git add cmd/wg/setconf.go cmd/wg/setconf_test.go cmd/wg/command.go
git commit -m "feat: implement wg configuration commands"
```

---

## 阶段 6：密钥命令

### 任务 16：实现 `genkey`、`genpsk` 和 `pubkey`

**文件：**
- 创建：`cmd/wg/key.go`
- 创建：`cmd/wg/key_test.go`
- 创建：`cmd/wg/key_unix.go`
- 创建：`cmd/wg/key_windows.go`
- 修改：`cmd/wg/command.go`

- [ ] **步骤 1：编写失败测试**

测试 `genkey` 输出 32-byte Base64 加换行且满足 clamp；`genpsk` 输出 32-byte Base64 加换行；`pubkey` 固定向量；空输入、非法 Base64、错误长度、非空白尾随内容失败，空白尾随内容成功。随机源注入失败时必须返回非零。Unix regular file world-readable warning做平台测试；Windows 只实现 oracle 在该平台确实可观测的行为。

- [ ] **步骤 2：运行测试验证失败**

运行：`go test ./cmd/wg -run 'TestGenkey|TestGenpsk|TestPubkey' -count=1`。

预期：命令尚未实现。

- [ ] **步骤 3：编写最少实现代码**

使用 `wgtypes.GeneratePrivateKey`、`wgtypes.GenerateKey` 和 `Key.PublicKey`；`pubkey` 严格读取 44 字符 Base64 key 后只允许空白或 EOF。输出只写一行。文件权限 warning 放到 build-tag 平台 helper。

- [ ] **步骤 4：运行测试验证通过**

运行：

```bash
go test ./cmd/wg -run 'TestGenkey|TestGenpsk|TestPubkey' -count=1
go test ./cmd/wg -count=1
```

预期：所有密钥和命令测试通过。

- [ ] **步骤 5：Commit**

```bash
git add cmd/wg/key.go cmd/wg/key_test.go cmd/wg/key_unix.go cmd/wg/key_windows.go cmd/wg/command.go
git commit -m "feat: implement wg key commands"
```

---

## 阶段 7：差分、平台和发布验收

### 任务 17：建立 C/Go 差分测试夹具

**文件：**
- 创建：`cmd/wg/differential_test.go`
- 创建：`cmd/wg/testdata/`
- 创建：`internal/wgconfig/oracle_test.go`
- 创建：`internal/wgcli/oracle_test.go`

- [ ] **步骤 1：编写失败测试**

通过 `WG_TOOLS_ORACLE` 环境变量接收已构建 C 版 `wg` 路径；未设置时 `t.Skip`。对纯输入命令直接比较 stdout、stderr、退出码；对设备输出使用同一固定状态 fixture 驱动 Go formatter，并由专用 oracle fixture 生成 C 输出。测试矩阵至少包括 help/version、所有 usage、无效参数、配置 parse 标记、show 字段、dump、showconf、hide keys 和 color mode。

- [ ] **步骤 2：运行测试验证失败**

运行：

```bash
WG_TOOLS_ORACLE=/path/to/wg go test ./cmd/wg ./internal/wgconfig ./internal/wgcli -run Oracle -count=1
```

预期：首次运行报告具体字节差异；禁止通过宽松 trim、忽略 stderr 或忽略退出码使测试通过。

- [ ] **步骤 3：逐项修正最小兼容差异**

每次只修一个 oracle 差异：usage、标点、空行、字段顺序、错误前缀、环境变量或退出码；为每个差异保留固定回归用例。若差异来自 OS 错误文本，比较稳定前缀和错误类别，并在测试中记录该平台差异，而不是伪造 errno 文本。

- [ ] **步骤 4：运行测试验证通过**

运行：

```bash
WG_TOOLS_ORACLE=/path/to/wg go test ./cmd/wg ./internal/wgconfig ./internal/wgcli -run Oracle -count=1
go test ./... -count=1
```

预期：oracle 差分和全量单元测试通过。

- [ ] **步骤 5：Commit**

```bash
git add cmd/wg internal/wgconfig internal/wgcli
git commit -m "test: compare wg behavior with wireguard-tools"
```

### 任务 18：替换旧诊断命令并更新 CI

**文件：**
- 删除：`cmd/wgctrl/main.go`（确认无外部构建脚本引用后）
- 修改：`.github/workflows/linux-test.yml`
- 修改：`.github/workflows/static-analysis.yml`
- 修改：`.github/workflows/linux-integration-test.yml`
- 修改：`CLAUDE.md` 中仅与命令路径相关的内容

- [ ] **步骤 1：编写失败检查**

运行：

```bash
go build -o wg ./cmd/wg
test -x ./wg
```

并增加 CI 断言 `go build ./cmd/wg`。在删除旧命令前用内容搜索确认 `.cibuild.sh`、workflow 和文档没有依赖 `cmd/wgctrl`。

- [ ] **步骤 2：验证现状不满足验收**

运行：`go list ./cmd/...`。

预期：同时列出 `cmd/wgctrl` 和 `cmd/wg`，尚未达到只维护正式 CLI 的职责边界。

- [ ] **步骤 3：实施最小清理**

删除旧诊断入口；CI 普通 job 增加三平台可执行文件构建，不在普通 job 启用破坏性集成测试。`CLAUDE.md` 将构建示例更新为 `go build ./cmd/wg`。

- [ ] **步骤 4：运行验证**

运行：

```bash
go list ./cmd/...
go build ./cmd/wg
go test -race ./...
go vet ./...
staticcheck ./...
GOOS=windows GOARCH=amd64 go build -o wg.exe ./cmd/wg
GOOS=darwin GOARCH=amd64 go build -o wg-darwin ./cmd/wg
```

预期：仅列出 `cmd/wg`；Linux 当前平台测试和静态检查通过；Windows/macOS 构建成功。

- [ ] **步骤 5：Commit**

```bash
git add cmd .github CLAUDE.md
git commit -m "build: replace diagnostic CLI with wg"
```

### 任务 19：Linux 专用接口集成验证

**文件：**
- 创建或修改：`cmd/wg/integration_linux_test.go`
- 修改：`.github/workflows/linux-integration-test.yml`

- [ ] **步骤 1：编写受保护的集成测试**

测试必须要求独立开关（例如 `WGCLI_INTEGRATION=yesreallydoit`）和明确接口名（例如 `WGCLI_TEST_INTERFACE=wgcli0`）；若接口名不符合专用前缀则拒绝运行。记录初始状态，测试结束恢复或删除由测试创建的配置。

- [ ] **步骤 2：在隔离 Linux 环境运行并验证初始失败**

运行：

```bash
sudo env WGCLI_INTEGRATION=yesreallydoit WGCLI_TEST_INTERFACE=wgcli0 \
  go test ./cmd/wg -run TestIntegration -v -count=1
```

预期：在 CLI 尚有状态差异时失败并显示 C/Go 最终状态差异；不得在已有生产接口上运行。

- [ ] **步骤 3：修复最小平台差异**

依次验证 `set`、`setconf`、`addconf`、`syncconf`、普通/`+`/`-` AllowedIPs、show/dump/showconf；每项用 C 版和 Go 版操作独立重置后的同一专用接口并比较最终状态。

- [ ] **步骤 4：运行完整 Linux 验证**

运行：

```bash
go test -race ./...
sudo env WGCLI_INTEGRATION=yesreallydoit WGCLI_TEST_INTERFACE=wgcli0 \
  go test ./cmd/wg -run TestIntegration -v -count=1
```

预期：单元测试和专用接口集成测试均通过，测试结束接口状态已恢复。

- [ ] **步骤 5：Commit**

```bash
git add cmd/wg/integration_linux_test.go .github/workflows/linux-integration-test.yml
git commit -m "test: validate wg CLI on Linux"
```

### 任务 20：Windows 和 macOS 平台验收

**文件：**
- 创建：`cmd/wg/integration_windows_test.go`
- 创建：`cmd/wg/integration_darwin_test.go`
- 修改：必要的平台终端或测试 helper

- [ ] **步骤 1：编写平台受保护测试**

Windows 只允许 `WGCLI_TEST_INTERFACE` 指向专用 WireGuardNT adapter；macOS 只允许专用 userspace socket。两者均要求显式开关并拒绝默认枚举到的已有接口。

- [ ] **步骤 2：在目标平台验证失败路径**

Windows：

```bash
go test ./... -count=1
$env:WGCLI_INTEGRATION='yesreallydoit'
$env:WGCLI_TEST_INTERFACE='wgcli-test'
go test ./cmd/wg -run TestIntegration -v -count=1
```

macOS：

```bash
go test ./... -count=1
sudo env WGCLI_INTEGRATION=yesreallydoit WGCLI_TEST_INTERFACE=wgcli-test \
  go test ./cmd/wg -run TestIntegration -v -count=1
```

预期：缺少测试 adapter/socket 时明确 skip 或前置失败，不接触其他接口。

- [ ] **步骤 3：验证平台真实能力**

Windows 比较 WireGuardNT 的状态、remove flag 和机器输出；macOS 比较 userspace socket 的状态、`allowed_ip=-CIDR` 和机器输出。保留生产 Windows named pipe `LocalSystem` owner 校验；测试连接继续使用现有 test-only dial。

- [ ] **步骤 4：执行最终平台矩阵**

每个平台运行：

```bash
go test ./... -count=1
go vet ./...
staticcheck ./...
go build -o wg ./cmd/wg
```

Linux 额外运行 `go test -race ./...`；Windows/macOS 运行各自专用集成测试。预期：全部成功，且无生产接口状态变化。

- [ ] **步骤 5：Commit**

```bash
git add cmd/wg/integration_windows_test.go cmd/wg/integration_darwin_test.go
git commit -m "test: validate wg CLI on Windows and macOS"
```

---

## 最终验收清单

- [ ] Linux、macOS、Windows 均构建名为 `wg`/`wg.exe` 的程序。
- [ ] 无参数执行等同 `wg show`。
- [ ] `show`、`showconf`、`set`、`setconf`、`addconf`、`syncconf`、`genkey`、`genpsk`、`pubkey`、help、version 均有参数和错误测试。
- [ ] 所有机器可读输出与 C oracle 字节级一致。
- [ ] pretty 输出的 peer 排序、密钥隐藏、时间、流量和颜色规则一致。
- [ ] `setconf` 清除缺失设备字段，`addconf` 保留未涉及设备字段，`syncconf` 使用当前状态生成删除和清零项。
- [ ] `+CIDR/-CIDR` 通过 Linux netlink、WireGuardNT、userspace 或平台等价协议直接编码；不使用非原子读改写模拟。
- [ ] 不接受 `Address`、`DNS`、`MTU`、`Table`、hooks、`SaveConfig`，不实现任何 `wg-quick` 行为。
- [ ] 单元测试、race、vet、staticcheck 和三平台构建均有新鲜成功证据。
- [ ] 集成测试只操作显式指定的专用测试接口，并在退出时恢复状态。

## 平台风险与控制

- **Linux netlink 分批：** AllowedIP 操作必须随 chunk 保留；`ReplacePeers` 仅首批、同一 peer 的 `ReplaceAllowedIPs` 仅首个 chunk。用超过 256 个 AllowedIPs 的测试锁定。
- **Windows 结构布局：** `AllowedIP.Flags` 会改变结构字段解释，必须与 `uapi/windows/wireguard.h` 的对齐和大小一致，并在 Windows 原生 amd64 环境验证。
- **Windows named pipe 安全：** 生产拨号继续要求 owner 为 `LocalSystem`；只能在 `_test.go` 使用当前用户 pipe 的 test dial。
- **macOS userspace：** 只通过现有 socket 后端，不创建接口；增量删除必须验证目标 userspace 实现接受 `allowed_ip=-CIDR`。
- **FreeBSD/OpenBSD：** 不属于主要目标平台，但公共模型变更不得使其无法编译；FreeBSD 原生 cgo 验证，OpenBSD 保持只读明确错误。
- **错误文本：** `perror` 的系统文本跨 OS 不稳定；固定 C 版稳定前缀、usage 和退出码，平台 errno 尾部在同平台差分。
- **版本字符串：** 当前 oracle 为 `1.0.20260223`；实现时集中定义，并在升级 oracle 时由单个测试暴露差异。
- **破坏性测试：** 现有根集成测试会重置所有设备，不能作为 CLI 日常验证；新增 CLI 集成测试必须绑定显式专用接口。
