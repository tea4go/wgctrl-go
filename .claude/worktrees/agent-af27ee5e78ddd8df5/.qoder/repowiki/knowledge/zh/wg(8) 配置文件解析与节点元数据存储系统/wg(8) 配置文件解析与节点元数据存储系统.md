---
kind: configuration_system
name: wg(8) 配置文件解析与节点元数据存储系统
category: configuration_system
scope:
    - '**'
source_files:
    - internal/wgconfig/parse.go
    - internal/wgconfig/encode.go
    - cmd/wg/config.go
    - internal/wgmeta/store.go
    - internal/wgmeta/path.go
    - client.go
    - os_linux.go
---

## 1. 使用的系统与方案

本仓库没有传统意义上的应用级配置框架（如 YAML/TOML/环境变量集中加载器），而是围绕 wg(8) 兼容的 WireGuard 配置文件和节点名称元数据文件构建了两套轻量配置机制：

- wg(8) 配置文件解析/编码：internal/wgconfig 包实现了 Parse / Encode，完全遵循 wireguard-tools 的 [Interface] / [Peer] 段式 .conf 文件格式。
- 节点名称元数据存储：internal/wgmeta 包使用 JSON 文件持久化每个接口下 peer 公钥到人类可读名称的映射，作为 wg CLI 的扩展能力。
- 运行时行为开关：仅通过少量环境变量控制解析时的 DNS 重试次数与元数据文件路径，不引入配置中心或 feature flag 系统。

## 2. 关键文件与包

- internal/wgconfig/parse.go — 逐行扫描 .conf 文件，支持 interface、peer 两个 section，字段包括 PrivateKey、ListenPort、FwMark、PublicKey、PresharedKey、Endpoint、AllowedIPs、PersistentKeepalive；非 append 模式下默认将 ReplacePeers 置为 true。
- internal/wgconfig/encode.go — 反向生成 showconf 兼容输出，并在 peer 段写入 # wgctrl-peer-name = <JSON> 注释以携带节点名称。
- cmd/wg/config.go — CLI 入口，调用 wgconfig.Parse 读取配置文件并通过 wgctrl.Client.ConfigureDevice 下发；同时维护 syncconf / setconf / addconf 三种模式（append/replace/sync）。
- internal/wgmeta/store.go — 基于 JSON 文件的 peer 名称存储，结构体 fileData 包含 Version 与 Interfaces map[interfaceName]map[publicKey]string]；读写加进程级互斥 processMu 与文件锁 lockFile。
- internal/wgmeta/path.go — 提供 DefaultPath()，按 OS 选择默认路径，并允许通过 WGCTRL_PEER_METADATA_FILE 环境变量覆盖。
- client.go + os_linux.go 等 os_*.go — 装配内核态 (wglinux) 与用户态 (wguser) 后端，对外暴露统一 Client；不属于“应用配置”但体现了跨平台后端装配模式。

## 3. 架构与设计约定

### 3.1 wg(8) 配置文件格式
- 文件由 [Interface] 与多个 [Peer] 段组成，键名大小写不敏感（strings.ToLower 处理）。
- 每行以 = 分割键值，# 后为注释；空白字符被 cleanLine 过滤。
- 特殊语法：
  - AllowedIPs 支持 , 分隔列表，且每项前缀 + / - 表示增量操作（AllowedIPAdd / AllowedIPRemove），否则为全量替换（AllowedIPSet）。
  - PersistentKeepalive 接受 off 关键字。
  - FwMark 接受十进制或 0x 十六进制，也支持 off。
  - Endpoint 支持 host:port 与 [ipv6]:port，解析失败时带指数退避重试 DNS（可配置重试次数）。
- 非 append 模式下，Parse 会初始化 cfg.PrivateKey、cfg.ListenPort、cfg.FirewallMark 为零值，并将 cfg.ReplacePeers = true，即默认替换整个设备配置。
- 每个 peer 必须出现 PublicKey，否则报错。

### 3.2 节点名称元数据（peer names）
- 通过 # wgctrl-peer-name = "name" 这种 wg(8) 注释在配置文件中声明 peer 名称。
- 解析时提取该注释，写入 internal/wgmeta.Store；显示时从 JSON 文件读回并附加到 wgtypes.Peer.Name。
- 元数据文件采用原子写入：先写临时文件再 os.Rename，权限设为 0o644，目录不存在则 MkdirAll(0755)。
- 并发安全：Store.Names / Store.Update 使用全局 processMu 串行化同一进程内访问；Update 额外获取 .lock 文件锁以保护多进程场景。
- 版本控制：fileData.Version 当前固定为 1，读取时校验版本号，不支持的版本直接报错。

### 3.3 环境变量配置
仓库中只暴露了极少数的运行时开关，全部通过 os.Getenv / os.LookupEnv 读取：
- WG_ENDPOINT_RESOLUTION_RETRIES：控制 endpoint 解析时的 DNS 重试次数。支持整数、infinity、以及 +N 形式；默认 15 次，解析失败按指数退避（上限 20s）重试。
- WGCTRL_PEER_METADATA_FILE：覆盖节点元数据 JSON 文件的默认路径。

### 3.4 后端装配（跨平台 Client）
- client.go 中的 Client 持有 []wginternal.Client 切片，依次尝试各后端；遇到 os.ErrNotExist 继续下一个，直到找到设备或全部失败。
- os_linux.go 优先装配内核态 wglinux，再追加用户态 wguser；其他 os_*.go 类似按平台组合后端。
- 这属于“运行时后端选择”而非“应用配置”，但与配置系统的 CLI 层紧密配合（CLI 通过 wgctrl.New() 获得 Client）。

## 4. 约定与约束

| 规则 | 来源/依据 |
|---|---|
| 配置文件必须使用 wg(8) 兼容的 [Interface] / [Peer] 段式文本格式 | internal/wgconfig/parse.go 的 section 解析逻辑 |
| 未知 section 或未知字段会直接报错 | parse.go 中对 default case 返回错误 |
| 每个 Peer 必须包含 PublicKey，否则解析失败 | parse.go 末尾对 peerHasPublicKey 的检查 |
| 非 append 模式下，ReplacePeers 默认为 true，PrivateKey/ListenPort/FirewallMark 默认零值 | parse.go 开头对 appendMode 分支的处理 |
| AllowedIPs 中 +/- 前缀表示增量操作，其余为全量替换 | parse.go parseAllowedIPs 中对首字符的判断 |
| PersistentKeepalive 与 FwMark 支持 off 关键字 | parse.go 中 parseUintOrOff / parseFwmark |
| Endpoint 解析失败时会带指数退避重试 DNS，默认最多 15 次 | parse.go resolveEndpoint 与 endpointResolutionRetries |
| 节点名称通过 # wgctrl-peer-name = <JSON> 注释传递，名称需 UTF-8 合法且 ≤255 字节、不含控制字符 | parse.go validatePeerName 与 encode.go 的 JSON 序列化 |
| 节点元数据文件默认位于 /var/lib/wgctrl-go/peer-names.json（Linux）、/Library/Application Support/wgctrl-go/...（macOS）、%ProgramData%/wgctrl-go/...（Windows），可通过 WGCTRL_PEER_METADATA_FILE 覆盖 | internal/wgmeta/path.go |
| 元数据文件写入是原子的（临时文件 + rename），权限 0644 | store.go write 方法 |
| 元数据文件存在版本字段，当前仅支持 version 1 | store.go read 方法 |
| 同一进程内对元数据的读写串行化，跨进程通过 .lock 文件锁保护 | store.go 中 processMu 与 lockFile |
| 后端选择顺序：Linux 上先内核态后用户态，遇到 os.ErrNotExist 跳过 | os_linux.go 与 client.go 的循环逻辑 |

## 5. 总结

该仓库的配置系统不是通用配置框架，而是针对 WireGuard 生态的专用实现：以 wg(8) 配置文件为核心输入，通过 internal/wgconfig 解析为 wgtypes.Config 并下发给内核/用户态后端；同时用 internal/wgmeta 的 JSON 元数据文件扩展出 peer 名称管理能力。配置来源仅限于本地 .conf 文件与极少数环境变量（DNS 重试次数、元数据路径），没有集中式配置服务、feature flag 或密钥管理组件。