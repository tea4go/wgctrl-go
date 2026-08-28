---
kind: build_system
name: Go 模块构建与多平台 CI 流水线
category: build_system
scope:
    - '**'
source_files:
    - .cibuild.sh
    - .builds/freebsd.yml
    - .builds/openbsd.yml
    - .github/workflows/linux-test.yml
    - .github/workflows/linux-integration-test.yml
    - .github/workflows/static-analysis.yml
    - go.mod.base
---

## 1. 构建系统与工具链

本项目是一个纯 Go 库（`golang.zx2c4.com/wireguard/wgctrl`），使用 **Go Modules** 管理依赖，固定最低版本为 `go 1.20`（见 `go.mod.base`）。仓库没有 Makefile、Dockerfile 或自定义发布脚本；所有构建、测试、静态检查均通过 GitHub Actions 和 Buildkite（`.builds/`）完成。

- 依赖清单：`go.mod.base` 定义模块名、Go 版本及全部直接/间接依赖；CI 中实际使用的 `go.sum` 由 `go mod tidy` 生成。
- CGO：FreeBSD/OpenBSD 构建显式设置 `CGO_ENABLED=1`，因为 BSD 后端需要调用系统 C ABI（如 `wgh` 头文件）。
- 交叉编译：OpenBSD 任务中包含 `GOARCH=386 go build ./...` 用于校验不同内核结构体大小，是仓库中唯一显式的交叉编译用例。

## 2. CI 流水线

### 2.1 GitHub Actions（Linux 为主）
三个 workflow 位于 `.github/workflows/`：

| 文件 | 触发 | 职责 |
|---|---|---|
| `linux-test.yml` | push / PR | `go test -race ./...`（仅 Linux 单元测试） |
| `linux-integration-test.yml` | push / PR | 先运行 `.cibuild.sh` 创建 `wg0` 接口并安装 `wireguard-go`，再启动用户态设备 `sudo wireguard-go wguser0`，最后以 `WGCTRL_INTEGRATION=yesreallydoit` 环境变量运行集成测试 |
| `static-analysis.yml` | push / PR | 安装 `staticcheck@HEAD` 并执行 `staticcheck ./...` + `go vet ./...` |

### 2.2 Buildkite（FreeBSD / OpenBSD）
位于 `.builds/freebsd.yml` 与 `.builds/openbsd.yml`，镜像分别为 `freebsd/latest` 与 `openbsd/latest`。两任务共用同一套流程：
1. 克隆源码到 `wgctrl-go/` 目录。
2. 执行 `./wgctrl-go/.cibuild.sh` 准备 WireGuard 内核接口（`ifconfig wg create` / `ip link add wg0 type wireguard`）。
3. 安装 `staticcheck`，运行 `go vet`、`staticcheck`、`go test -v -race ./...`（OpenBSD 跳过 `-race`）。
4. 编译测试二进制 `go test -c .`。
5. 启动 `wireguard-go` 用户态设备作为额外测试环境。
6. 以 root 权限运行集成测试：`sudo doas WGCTRL_INTEGRATION=yesreallydoit ./wgctrl.test -test.v -test.run TestIntegration`。

## 3. 集成测试前置脚本

`.cibuild.sh` 是跨平台集成测试的共享入口，职责包括：
- 根据 `uname -s` 选择 sudo 命令（OpenBSD 用 `doas`）。
- 按 OS 创建并启用 WireGuard 虚拟接口：Linux 用 `ip link add wg0 type wireguard`，FreeBSD/OpenBSD 用 `ifconfig wg create`。
- 从上游 `git clone https://git.zx2c4.com/wireguard-go` 拉取 wireguard-go，在 Linux 上走 `make`，在其他平台直接 `go build -o wireguard-go`，然后 `mv` 到 `/usr/local/bin/wireguard-go`。

该脚本被 GitHub Actions 与 Buildkite 共同复用，确保三平台测试环境一致。

## 4. 构建约定与约束

- **Go 版本锁定**：所有 CI 矩阵固定 `go-version: ["1.20"]`，与 `go.mod.base` 中的 `go 1.20` 保持一致。
- **模块开关**：Buildkite 任务显式设置 `GO111MODULE=on`，强制使用 Go Modules。
- **CGO 要求**：BSD 构建必须开启 `CGO_ENABLED=1`，否则无法编译 `internal/wgfreebsd` 与 `internal/wgopenbsd` 下的 C 绑定代码。
- **集成测试开关**：集成测试默认不运行，需设置环境变量 `WGCTRL_INTEGRATION=yesreallydoit` 才会执行（由 `client_integration_test.go` 中的构建标签/条件控制）。
- **静态检查统一标准**：`staticcheck` 版本在 Linux 上使用 `@HEAD`，在 FreeBSD 上使用 `@latest`，但都作为质量门禁。
- **无发布产物**：仓库不包含 `Makefile release`、`Dockerfile`、`VERSION` 文件或版本号注入逻辑；发布流程未在此仓库中体现，推测由上层仓库或外部脚本负责。

## 5. 关键文件

- `.cibuild.sh` — 跨平台集成测试环境准备（创建 WireGuard 接口、安装 wireguard-go）
- `.builds/freebsd.yml` — Buildkite FreeBSD 构建任务
- `.builds/openbsd.yml` — Buildkite OpenBSD 构建任务（含 32 位交叉编译检查）
- `.github/workflows/linux-test.yml` — Linux 单元测试（带 race detector）
- `.github/workflows/linux-integration-test.yml` — Linux 集成测试（启动 wireguard-go 用户态设备）
- `.github/workflows/static-analysis.yml` — staticcheck + go vet 静态分析
- `go.mod.base` — 模块声明与依赖清单
