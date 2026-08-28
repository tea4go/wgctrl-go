---
kind: dependency_management
name: Go 模块依赖管理（go.mod/go.sum + go.mod.base 双文件策略）
category: dependency_management
scope:
    - '**'
source_files:
    - go.mod
    - go.sum
    - go.mod.base
---

## 1. 使用的系统/方法

本项目采用 Go Modules（`go.mod` / `go.sum`）作为唯一的第三方依赖管理机制，未使用 vendor 目录、GOPATH 模式或私有代理。所有依赖通过标准 `require` 块声明，并通过 `go.sum` 锁定每个依赖的精确哈希校验值。

## 2. 关键文件

- `go.mod`：当前工作区的模块定义与依赖清单，模块路径为 `golang.zx2c4.com/wireguard/wgctrl`，最低 Go 版本要求为 `1.20`。
- `go.sum`：所有直接和间接依赖的精确哈希（h1）及对应 `go.mod` 摘要，用于确保可重复构建。
- `go.mod.base`：一个与 `go.mod` 内容完全相同的基线副本，用于 CI 或发布流程中检测依赖是否被意外修改（diff 对比）。该文件在仓库根目录存在，表明项目维护者有意将依赖变更纳入版本控制审查。

## 3. 架构与约定

- **依赖分类**：直接依赖放入顶层 `require` 块，间接依赖以 `// indirect` 注释标记并归入第二个 `require` 块，便于区分由工具链自动引入的传递依赖。
- **版本策略**：所有依赖均锁定到具体版本号（含 git pseudo-version），如 `golang.org/x/crypto v0.31.0`、`github.com/mdlayher/netlink v1.7.2`、`golang.zx2c4.com/wireguard v0.0.0-20231211153847-12269c276173`，不依赖 `latest` 或通配符。
- **平台相关依赖**：Linux 后端通过 `mdlayher/netlink` 与内核 netlink 通信；FreeBSD/OpenBSD/Windwos 后端通过 `golang.org/x/sys` 调用系统 API；CLI 终端交互使用 `golang.org/x/term`。这些依赖按构建标签（build tag）在不同 `os_*.go` 文件中按需引入，避免在非目标平台产生不必要的编译开销。
- **内部依赖**：项目自身依赖同组织下的 `golang.zx2c4.com/wireguard` 包（v0.0.0-20231211153847-12269c276173 的特定提交），表明 wgctrl 与 wireguard-go 保持紧密耦合，升级需同步评估兼容性。
- **测试依赖**：`github.com/google/go-cmp` 仅用于测试比较，未出现在运行时依赖中。

## 4. 约定与约束

- **无 vendor 目录**：仓库未包含 `vendor/`，依赖全部通过 Go Modules 下载缓存获取。
- **无 GOPRIVATE / 私有代理配置**：`go.mod` 中未出现 `replace` 指令或 `GOPRIVATE` 环境变量引用，所有依赖均来自公共 Go 模块代理或 GitHub。
- **CI 中的依赖保护**：`.builds/freebsd.yml`、`.builds/openbsd.yml` 以及 `.github/workflows/*.yml` 等 CI 脚本配合 `go.mod.base` 使用，可在构建前比对 `go.mod` 与基线差异，防止未经审查的依赖变更进入分支。
- **最小 Go 版本约束**：`go 1.20` 明确限定运行环境，确保依赖（尤其是 `golang.org/x/*` 系列）与语言特性兼容。
- **间接依赖显式化**：`josharian/native`、`mdlayher/socket`、`golang.org/x/net`、`golang.org/x/sync` 等传递依赖均以 `// indirect` 标注，体现“仅显式声明所需”的约定。

总体而言，该项目遵循标准的 Go Modules 实践，并通过 `go.mod.base` 这一额外基线文件强化了依赖变更的可审计性，适合对安全与可复现性要求较高的系统级库。