---
kind: logging_system
name: 无专用日志系统：仅使用标准库 log 与 fmt 输出
category: logging_system
scope:
    - '**'
source_files:
    - cmd/wgctrl/main.go
    - cmd/wg/show.go
    - internal/wgcli/format.go
---

## 1. 使用的系统/方法

本仓库**没有引入任何第三方日志框架**（如 zap、logrus、slog、logr 等），也没有自定义 logger 抽象或结构化日志中间件。所有日志/诊断输出均直接使用 Go 标准库 `fmt` 和 `log`，且仅出现在极少数 CLI 入口路径中。

- `cmd/wgctrl/main.go`：使用 `log.Fatalf(...)` 打印启动失败、设备查询失败等致命错误，并直接终止进程。
- `cmd/wg/show.go`、`internal/wgcli/format.go`：通过 `fmt.Fprint` / `fmt.Printf` 向 stdout/stderr 输出格式化结果（属于命令输出而非日志）。
- 其余代码路径（核心 client、各平台后端、配置解析、测试辅助等）**不产生任何日志输出**，错误一律以 error 返回值向上冒泡，由调用方决定如何处理。

## 2. 关键文件

| 文件 | 作用 |
|---|---|
| `cmd/wgctrl/main.go` | 唯一使用 `log.Fatalf` 的位置，用于 CLI 致命错误 |
| `cmd/wg/show.go` | 使用 `fmt.Fprint` 输出设备信息（stdout） |
| `internal/wgcli/format.go` | 终端格式化输出（stdout） |
| 其他所有包 | 无日志调用；错误通过 error 返回 |

## 3. 架构与约定

- **无全局 logger**：不存在 `logger`、`Logger`、`SetLevel`、`Init` 之类的初始化逻辑。
- **无日志级别**：未定义 debug/info/warn/error 等分级概念。
- **无结构化字段**：没有 key-value 形式的日志字段，所有消息均为纯字符串。
- **错误即日志**：非 CLI 路径的错误全部作为 error 返回值返回给上层；CLI 层在必要时用 `log.Fatalf` 直接退出。
- **输出目标单一**：CLI 的“日志”走 stderr（`log` 默认），正常输出走 stdout（`fmt.Fprint`/`Printf`）。没有 sink、file writer、syslog 等路由机制。

## 4. 约定与约束

- **库代码不写日志**：`client.go`、`internal/wg*` 各平台后端、`wgtypes`、`internal/wgmeta` 等库级包均未包含任何 `log`/`fmt.Print*` 调用，遵循“库只返回 error，由使用者决定如何记录”的约定。
- **CLI 仅对致命错误使用 `log.Fatalf`**：`cmd/wgctrl/main.go` 仅在无法打开 wgctrl、无法获取设备等不可恢复错误时调用 `log.Fatalf`，不会记录普通调试信息。
- **测试辅助使用 panic**：`internal/wgtest`、各平台 `_test.go` 中的断言失败通过 `panic(fmt.Sprintf(...))` 表达，这是测试代码的惯用方式，不属于运行时日志。
- **无配置文件控制日志行为**：仓库中没有 `LOG_LEVEL`、`LOG_FILE`、`--verbose` 等环境变量或命令行标志来调节日志输出。

综上，该仓库在当前实现下**不存在独立的日志子系统**；日志相关实践仅限于 CLI 入口处的少量 `log.Fatalf` 与 `fmt` 输出，核心库完全依赖 error 返回值传递异常信息。