# 构建版本自增与构建时间设计

## 目标

统一 `wg` 和 `wgd` 的构建信息，使所有正式构建入口具备以下行为：

- 每次构建生成一个版本号和一个构建时间。
- 同一次构建产生的 `wg`、`wgd` 使用完全相同的版本号和构建时间。
- Windows、Linux 和 macOS 目标保持一致。
- 构建信息可通过 CLI 和 REST API 查询。

## 构建入口

本次覆盖以下入口：

- `buildapp.ps1`
- `runwin.cmd`
- `runlinux.cmd`
- `runlinux.sh`

`runwin.cmd` 和 `runlinux.cmd` 继续委托 `buildapp.ps1`，不重复实现版本算法。
`runlinux.sh` 独立执行相同的版本解析、自增和注入流程。

## 版本规则

`VERSION.txt` 保存最近一次自动生成的版本。未设置 `APP_TAG` 时：

1. 读取 `VERSION.txt`，文件不存在或内容无效时以 `v3.0.0` 为起点。
2. 构建开始时只递增一次版本。
3. PATCH、MINOR 按十进位进位：
   - `v3.0.9` 的下一版本为 `v3.1.0`。
   - `v3.9.9` 的下一版本为 `v4.0.0`。
4. 将新版本写回 `VERSION.txt`。
5. 使用同一个新版本构建 `wg` 和 `wgd`。

设置 `APP_TAG` 时，直接使用其值，不读取后自增，也不更新 `VERSION.txt`。

`IS_BETA=true` 时，沿用现有规则生成
`<APP_TAG>_B<yyyyMMdd>_<HHmm>`；正式版只使用 `APP_TAG`。

## 构建时间

每次构建只获取一次当前时间，格式固定为：

```text
yyyy-MM-dd(HH:mm:ss)
```

构建脚本通过 Go 链接参数向两个程序注入：

```text
-X main.version=<版本>
-X main.BuildTime=<构建时间>
```

`wgd` 的版本值保留 `wgctrl-go wgd` 前缀。`wg` 的版本值保留
`wireguard-tools` 标识，避免改变现有命令身份。

未通过正式构建脚本编译时，程序将空构建时间显示为 `unknown`。

## 运行时输出

### wg

`wg -v`、`wg --version` 和 `wg version` 输出：

```text
wireguard-tools <版本>
Build Time : <构建时间或 unknown>
Platform   : <GOOS>-<GOARCH>
```

### wgd

`GET /api/v1/version` 保留现有 `version` 字段，并增加：

```json
{
  "version": "wgctrl-go wgd <版本>",
  "build_time": "2026-08-26(20:30:00)",
  "platform": "linux-amd64"
}
```

新增字段不移除或改名，现有只读取 `version` 的客户端无需修改。

## 错误处理

- `VERSION.txt` 无法写入时立即终止构建，避免二进制版本与版本文件不一致。
- 任一二进制构建失败时返回非零退出码，不继续报告整体成功。
- `APP_TAG` 为空白时按未设置处理。
- 构建时间和版本参数必须作为单个 `ldflags` 值传递，避免时间中的空格或括号被拆分。

## 验证

实现按测试驱动方式完成：

1. 先增加 `wg` 版本输出测试和 `wgd` 版本响应字段测试，并确认测试因功能缺失而失败。
2. 实现最小运行时代码并确认测试通过。
3. 使用临时 `VERSION.txt` 或隔离目录验证版本自增、十进位进位和 `APP_TAG` 跳过自增。
4. 执行 `buildapp.ps1 -OS linux -Arch amd64`，确认 `wg`、`wgd` 携带相同版本和构建时间。
5. 执行 `runlinux.sh`，确认 Bash 构建入口具有相同行为。
6. 运行 `go test ./... -count=1`、Linux 交叉编译和 `git diff --check`。

## 范围限制

- 不修改版本号的十进位进位规则。
- 不引入新的版本文件或第三方构建工具。
- 不改变 `runlinux.cmd` 现有上传目标和部署流程。
- 不调整与构建信息无关的 CLI、REST 或日志行为。
