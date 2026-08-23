---
kind: error_handling
name: wgctrl-go 错误处理体系：哨兵错误、os.ErrNotExist 语义与平台错误归一化
category: error_handling
scope:
    - '**'
source_files:
    - wgtypes/errors.go
    - client.go
    - internal/wglinux/client_linux.go
    - internal/wguser/client.go
    - internal/wgmeta/store.go
    - internal/wgconfig/parse.go
    - internal/wgconfig/encode.go
    - cmd/wg/main.go
---

## 1. 使用的系统与总体方法

仓库采用 Go 标准库的错误模型，没有引入第三方错误框架。核心约定是：**通过 `errors.New` 定义包级哨兵错误（sentinel errors），通过 `fmt.Errorf("...: %w", err)` 包装并保留错误链，调用方使用 `errors.Is` / `errors.As` 进行判断**。对于“设备不存在”这一跨平台常见场景，统一返回 `os.ErrNotExist`，以便上层用 `errors.Is(err, os.ErrNotExist)` 判定。

## 2. 关键文件与位置

- **公共哨兵错误**：`wgtypes/errors.go` 定义了 `ErrUpdateOnlyNotSupported`，表示当前平台内核不支持 PeerConfig 的 UpdateOnly 标志。
- **顶层 Client 聚合层**：`client.go` 中的 `Client.Device` / `ConfigureDevice` 遍历多个后端实现，对每个后端返回的错误执行统一的 switch：成功则返回；若为 `os.ErrNotExist` 则继续尝试下一个后端；其他错误直接向上返回；所有后端都失败时返回 `os.ErrNotExist`。
- **Linux 内核态后端**：`internal/wglinux/client_linux.go` 在 `execute` 中将 netlink 的 `*netlink.OpError` 解包，把 `unix.ENODEV`、`unix.ENOTSUP` 映射为 `os.ErrNotExist`，其他错误（如 EPERM）直接透传；非 `*netlink.OpError` 的情况会构造带 bug 报告链接的 `fmt.Errorf` 错误。
- **用户态后端**：`internal/wguser/client.go` 在找不到设备时返回 `os.ErrNotExist`。
- **元数据存储**：`internal/wgmeta/store.go` 在读取不存在的元数据文件时显式 `errors.Is(err, os.ErrNotExist)` 并返回空结构而非错误，体现“缺失即默认值”的约定。
- **配置解析器**：`internal/wgconfig/parse.go`、`encode.go` 大量使用 `fmt.Errorf("line %d: ...", lineNo, ...)` 携带行号上下文，并用 `%w` 包装底层错误。
- **CLI 入口**：`cmd/wg/main.go` 仅调用 `execute` 并通过 `os.Exit` 退出码暴露结果，错误由 `execute` 内部返回的 error 经上层格式化输出。

## 3. 架构与约定

### 3.1 错误分类与传播

| 错误类型 | 产生位置 | 传播方式 | 调用方处理方式 |
|---|---|---|---|
| 设备不存在 | Linux/netlink (`ENODEV`/`ENOTSUP`)、用户态后端未找到、空接口名 | 统一转为 `os.ErrNotExist` | `errors.Is(err, os.ErrNotExist)` 跳过或重试 |
| 平台能力不足 | `wgtypes.ErrUpdateOnlyNotSupported` | 包级哨兵变量 | `errors.Is(err, wgtypes.ErrUpdateOnlyNotSupported)` |
| 权限/系统调用失败 | Linux netlink 操作 | 透传底层 `syscall.Errno` | 由调用方决定策略 |
| 配置解析错误 | `internal/wgconfig/*` | `fmt.Errorf("line %d: ...", lineNo, ...)` 包装 | 向 CLI 展示具体行号 |
| 元数据文件缺失 | `internal/wgmeta/store.go` | 视为“无数据”，返回空 map | 调用方按空配置处理 |

### 3.2 多后端聚合的错误路由

`client.go` 的 `Client` 维护一个 `[]wginternal.Client` 列表（按构建标签装配不同 OS 后端）。`Device` / `ConfigureDevice` 采用 **“尝试下一个后端”** 模式：遇到 `os.ErrNotExist` 就 continue，遇到其他错误立即返回，全部失败后回退到 `os.ErrNotExist`。这使得同一 API 能在同时存在内核态和用户态实现的系统上自动选择可用后端。

### 3.3 平台错误归一化

Linux 后端在 `execute` 中集中处理 netlink 错误：
- 将内核语义的 `ENODEV`、`ENOTSUP` 归一化为 `os.ErrNotExist`；
- 其他 errno（如 EPERM）原样透出，让调用方区分“不存在”和“无权限”；
- 如果底层返回了非预期的错误类型，构造包含 bug 追踪 URL 的 `fmt.Errorf`，便于定位异常路径。

### 3.4 错误包装风格

- 业务/解析类错误：使用 `fmt.Errorf("line %d: unknown interface field %q=%q", lineNo, key, value)` 这类带上下文的字符串 + `%w` 包装。
- 资源 I/O 类错误：`internal/wgmeta/store.go` 在 `read()` 中对 `os.ErrNotExist` 做特判，其他错误用 `fmt.Errorf("读取节点名称元数据 %s: %w", s.path, err)` 包装。
- 测试辅助：各后端测试文件中存在 `panicf` 辅助函数用于测试断言失败时 panic，但生产代码不使用 panic 作为控制流。

### 3.5 Panic / Recover 策略

仓库中 `panic` 仅出现在测试辅助函数（如 `internal/wgtest/wgtest.go`、各后端 test 文件中的 `panicf`）以及集成测试超时保护中。**生产代码不使用 panic/recover 进行错误恢复**，所有异常路径均通过 error 返回值表达。

## 4. 约定与约束

1. **设备不存在必须返回 `os.ErrNotExist`**：`client.go` 注释明确说明 `Device` / `ConfigureDevice` 返回的错误可通过 `errors.Is(err, os.ErrNotExist)` 检查；Linux 后端将 ENODEV/ENOTSUP 映射为此哨兵。
2. **平台能力差异使用哨兵错误**：`wgtypes.ErrUpdateOnlyNotSupported` 作为包级变量暴露，调用方可用 `errors.Is` 检测。
3. **禁止直接暴露底层错误类型给调用方**：Linux 后端注释写明 “We don't want to expose netlink errors directly to callers so unpack to something more generic”，因此 netlink 错误被解包后再决定是归一化还是透传。
4. **解析错误必须携带行号上下文**：`internal/wgconfig/parse.go` 对所有解析失败点使用 `fmt.Errorf("line %d: ...", lineNo, ...)`，形成一致的诊断格式。
5. **缺失配置文件视为空配置**：`internal/wgmeta/store.go` 对 `os.ErrNotExist` 返回零值结构，而不是向上抛错，体现“缺省即空”的约定。
6. **错误链必须可追溯**：所有包装使用 `%w` 而非 `%v`，保证 `errors.Is` 能穿透多层包装。
7. **CLI 层不吞错误**：`cmd/wg/main.go` 将 `execute` 的返回值直接传给 `os.Exit`，错误以退出码形式暴露，具体消息由 `execute` 内部打印。
8. **测试中使用 panic 作为断言失败信号**：测试辅助 `panicf` 仅在测试中用于快速失败，生产代码不依赖 panic/recover 机制。

## 5. 总结

该仓库的错误处理体系围绕三个支柱建立：**`os.ErrNotExist` 作为“设备不存在”的统一语义**、**包级哨兵错误表达平台能力限制**、**`fmt.Errorf(..., %w)` 包装保留错误链**。顶层 `Client` 在多后端间路由错误，Linux 后端负责将内核 errno 归一化，配置解析器提供带行号的诊断信息，元数据存储对缺失文件采取宽容策略。整个体系没有使用 panic 作为正常控制流，也没有引入第三方错误库，保持了 Go 原生错误模型的简洁性与可组合性。