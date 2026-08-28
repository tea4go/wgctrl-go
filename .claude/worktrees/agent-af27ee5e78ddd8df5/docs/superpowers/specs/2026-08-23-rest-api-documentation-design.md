# REST API 文档设计

## 目标

为当前 `wgd` REST 服务生成一份可直接供调用方使用的中文接口文档。

文档以仓库当前代码行为为唯一依据，不把设计目标、待修复问题或尚未实现的行为描述为现有能力。

## 输出文件

- 正式文档：`docs/rest-api.md`
- 本次不修改 REST 服务实现，不生成 OpenAPI 文件，不生成测试脚本。

## 内容结构

正式文档按以下顺序组织：

1. 服务概述和适用范围。
2. `wgd` 启动命令、启动参数和默认值。
3. 基础 URL、内容类型、密钥显示和请求体大小等通用约定。
4. REST 端点总览。
5. `Device`、`Peer`、`Config`、`PeerConfig` 数据模型。
6. 每个端点的请求方式、路径、参数、请求示例、成功响应和错误响应。
7. `set`、`add`、`sync` 3 种配置模式的差异。
8. HTTP 状态码和统一错误结构。
9. PowerShell `Invoke-RestMethod`、`Invoke-WebRequest` 调用示例。
10. 当前实现的已知限制和安全注意事项。

## 接口范围

文档覆盖以下现有端点：

```text
GET    /api/v1/health
GET    /api/v1/version
GET    /api/v1/interfaces
GET    /api/v1/devices
GET    /api/v1/devices/{name}
POST   /api/v1/devices/{name}
GET    /api/v1/devices/{name}/conf
PUT    /api/v1/devices/{name}/conf
POST   /api/v1/devices/{name}/conf
POST   /api/v1/genkey
POST   /api/v1/genpsk
POST   /api/v1/pubkey
```

`GET /api/v1/health` 等注释声明的方法与代码实际接受的方法存在差异时，文档以声明方法作为推荐用法，并在“已知限制”中说明当前处理器未严格限制 HTTP 方法。

## 示例约定

- 基础地址统一使用 `http://127.0.0.1:8080`。
- WireGuard 设备名统一使用 `wg0`。
- 示例密钥使用明确标注的占位值，不写入真实密钥。
- JSON 示例使用与 Go 结构体标签一致的 snake_case 字段。
- 配置文件示例使用 `text/plain` 和标准 `wg(8)` 配置格式。
- PowerShell 示例优先使用 `Invoke-RestMethod`；读取原始配置文本时使用 `Invoke-WebRequest`。

## 现状说明

文档必须明确记录以下当前行为：

- 服务默认只监听 `127.0.0.1:8080`，但可通过 `-listen` 暴露到其他地址。
- 服务当前没有认证和 TLS。
- `-hide-keys` 只影响设备 JSON，不能阻止配置文本接口返回密钥。
- JSON 中缺省字段表示不修改；当前实现无法区分缺省与显式 `null`。
- 结构化 JSON 配置不支持 AllowedIPs 的逐项增加和删除操作。
- 结构化 JSON 的端口、fwmark 和 keepalive 范围校验弱于配置文本入口。
- JSON 和配置文本请求体上限为 4 MiB，配置文本单行上限为 1 MiB。
- 部分处理器当前未严格校验 HTTP 方法。
- `sync` 当前只补充删除目标配置中不存在的 peer。

## 验证标准

正式文档完成后执行以下检查：

1. 对照 `internal/wgapi/server.go`，确认端点、方法、默认 mode 和状态码无遗漏。
2. 对照 `internal/wgapi/json.go`，确认所有 JSON 字段、类型和可选性无遗漏。
3. 对照 `cmd/wgd/main.go`，确认启动参数和默认值准确。
4. 执行占位标记扫描，结果必须为空。
5. 检查所有 JSON 示例可以被 PowerShell `ConvertFrom-Json` 解析。
6. 运行 `go test ./internal/wgapi ./cmd/wgd -count=1`，确认仅新增文档未影响现有代码。
