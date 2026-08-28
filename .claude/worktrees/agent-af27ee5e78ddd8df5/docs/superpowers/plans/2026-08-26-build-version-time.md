# 构建版本自增与构建时间实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 subagent-driven-development（推荐）或 executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。

**目标：** 让 `wg`、`wgd` 共享自动递增的构建版本和构建时间，并通过 CLI 与 REST API 对外显示。

**架构：** 两个 `package main` 各自定义可由 `-ldflags -X` 注入的 `version`、`BuildTime` 字符串。`buildapp.ps1` 和 `runlinux.sh` 每次只解析一次版本、获取一次时间，再用同一组值构建两个程序；`wg` 负责格式化 CLI 版本输出，`wgd` 将构建信息传给 `internal/wgapi` 的版本响应。

**技术栈：** Go 1.21+、PowerShell 5.1、Bash、Go `-ldflags -X`、标准库 `runtime`。

---

## 文件结构

- 修改 `cmd/wg/command.go`：保存可注入版本和构建时间，格式化 CLI 版本信息。
- 修改 `cmd/wg/command_test.go`：验证默认构建信息和注入后的版本输出。
- 修改 `cmd/wg/main_test.go`：验证 `-v` 使用格式化后的版本信息。
- 修改 `cmd/wgd/main.go`：定义构建时间并把版本、时间、平台传给 REST 服务。
- 创建 `cmd/wgd/main_test.go`：验证 `wgd` 生成的构建信息。
- 修改 `internal/wgapi/server.go`：保存构建时间和平台，并扩展版本响应。
- 修改 `internal/wgapi/server_test.go`：验证兼容的 `version` 字段及新增字段。
- 修改 `buildapp.ps1`：一次自增版本，并向两个二进制注入相同版本和时间。
- 修改 `runlinux.sh`：实现与 PowerShell 一致的版本自增、Beta 版本和注入规则。
- 保持 `runwin.cmd`、`runlinux.cmd` 为 `buildapp.ps1` 的薄包装，不重复版本逻辑。

### 任务 1：为 `wg` 增加可注入构建信息

**文件：**
- 修改：`cmd/wg/command.go`
- 修改：`cmd/wg/command_test.go`
- 修改：`cmd/wg/main_test.go`

- [ ] **步骤 1：编写失败的版本输出测试**

在 `cmd/wg/command_test.go` 增加测试，临时替换包变量并在清理时恢复：

```go
func TestVersionTextIncludesBuildMetadata(t *testing.T) {
	oldVersion, oldBuildTime := version, BuildTime
	t.Cleanup(func() {
		version = oldVersion
		BuildTime = oldBuildTime
	})

	version = "v4.1.0"
	BuildTime = "2026-08-26(21:00:00)"

	got := versionText()
	for _, want := range []string{
		"wireguard-tools v4.1.0",
		"Build Time : 2026-08-26(21:00:00)",
		"Platform   : " + runtime.GOOS + "-" + runtime.GOARCH,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("versionText()=%q, missing %q", got, want)
		}
	}
}

func TestVersionTextUsesUnknownBuildTime(t *testing.T) {
	oldBuildTime := BuildTime
	t.Cleanup(func() { BuildTime = oldBuildTime })
	BuildTime = ""

	if got := versionText(); !strings.Contains(got, "Build Time : unknown") {
		t.Fatalf("versionText()=%q", got)
	}
}
```

更新现有版本测试，使期望值调用 `versionText()`，不再直接比较旧常量。

- [ ] **步骤 2：运行测试并确认正确失败**

运行：

```powershell
go test ./cmd/wg -run 'TestVersionText|TestExecuteVersion|TestRunShowsVersion' -count=1
```

预期：编译失败，提示 `BuildTime` 或 `versionText` 未定义，证明缺少构建信息功能。

- [ ] **步骤 3：实现最小版本格式化**

在 `cmd/wg/command.go` 中将版本改为可注入变量，并增加格式化函数：

```go
var (
	version   = "v1.0.20260223"
	BuildTime = ""
)

func versionText() string {
	buildTime := BuildTime
	if buildTime == "" {
		buildTime = "unknown"
	}
	return fmt.Sprintf(
		"wireguard-tools %s - https://git.zx2c4.com/wireguard-tools/\n"+
			"Build Time : %s\n"+
			"Platform   : %s-%s\n",
		version,
		buildTime,
		runtime.GOOS,
		runtime.GOARCH,
	)
}
```

让 `execute` 的 `version` 分支和 `cmd/wg/main.go` 的 `-v/--version` 分支都输出 `versionText()`。

- [ ] **步骤 4：运行测试并确认通过**

运行：

```powershell
go test ./cmd/wg -run 'TestVersionText|TestExecuteVersion|TestRunShowsVersion' -count=1
```

预期：PASS。

### 任务 2：扩展 `wgd` REST 版本响应

**文件：**
- 修改：`internal/wgapi/server.go`
- 修改：`internal/wgapi/server_test.go`
- 修改：`cmd/wgd/main.go`
- 创建：`cmd/wgd/main_test.go`

- [ ] **步骤 1：编写失败的 REST 响应测试**

在 `internal/wgapi/server_test.go` 中新增：

```go
func TestVersionIncludesBuildInfo(t *testing.T) {
	s := New(
		&fakeClient{},
		"",
		Version("wgctrl-go wgd v4.1.0"),
		BuildInfo("2026-08-26(21:00:00)", "linux-amd64"),
	)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)

	var got map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"version":    "wgctrl-go wgd v4.1.0",
		"build_time": "2026-08-26(21:00:00)",
		"platform":   "linux-amd64",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("version response mismatch (-want +got):\n%s", diff)
	}
}
```

在 `cmd/wgd/main_test.go` 中新增：

```go
func TestRuntimeBuildInfo(t *testing.T) {
	oldVersion, oldBuildTime := version, BuildTime
	t.Cleanup(func() {
		version = oldVersion
		BuildTime = oldBuildTime
	})

	version = "v4.1.0"
	BuildTime = ""
	gotVersion, gotTime, gotPlatform := runtimeBuildInfo()
	if gotVersion != "wgctrl-go wgd v4.1.0" {
		t.Fatalf("version=%q", gotVersion)
	}
	if gotTime != "unknown" {
		t.Fatalf("buildTime=%q", gotTime)
	}
	if gotPlatform != runtime.GOOS+"-"+runtime.GOARCH {
		t.Fatalf("platform=%q", gotPlatform)
	}
}
```

- [ ] **步骤 2：运行测试并确认正确失败**

运行：

```powershell
go test ./internal/wgapi ./cmd/wgd -run 'TestVersionIncludesBuildInfo|TestRuntimeBuildInfo' -count=1
```

预期：编译失败，提示 `BuildInfo`、`BuildTime` 或 `runtimeBuildInfo` 未定义。

- [ ] **步骤 3：实现 REST 构建信息**

在 `internal/wgapi/server.go` 的 `Server` 中增加：

```go
buildTime string
platform  string
```

增加选项：

```go
func BuildInfo(buildTime, platform string) Option {
	return func(s *Server) {
		s.buildTime = buildTime
		s.platform = platform
	}
}
```

将版本响应改为：

```go
writeJSON(w, http.StatusOK, map[string]string{
	"version":    s.version,
	"build_time": s.buildTime,
	"platform":   s.platform,
})
```

在 `cmd/wgd/main.go` 中定义并使用：

```go
var (
	version   = "v1.0.20260223"
	BuildTime = ""
)

func runtimeBuildInfo() (string, string, string) {
	buildTime := BuildTime
	if buildTime == "" {
		buildTime = "unknown"
	}
	return "wgctrl-go wgd " + version, buildTime, runtime.GOOS + "-" + runtime.GOARCH
}
```

主函数调用 `runtimeBuildInfo()`，并把结果传入
`wgapi.Version(...)`、`wgapi.BuildInfo(...)`。

- [ ] **步骤 4：运行测试并确认通过**

运行：

```powershell
go test ./internal/wgapi ./cmd/wgd -run 'TestVersionIncludesBuildInfo|TestRuntimeBuildInfo|TestVersion' -count=1
```

预期：PASS，现有 `version` 字段测试仍通过。

### 任务 3：让 PowerShell 构建同时注入两个程序

**文件：**
- 修改：`buildapp.ps1`
- 验证：`runwin.cmd`
- 验证：`runlinux.cmd`

- [ ] **步骤 1：记录当前失败行为**

使用固定版本避免修改 `VERSION.txt`：

```powershell
$env:APP_TAG = 'v9.8.7'
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\buildapp.ps1 -OS linux -Arch amd64
.\wg.exe -v
```

若构建输出为 Linux 文件名，则在 Linux 主机或 WSL 中执行 `./wg -v`。

预期：当前 `wg` 不显示 `v9.8.7` 和构建时间；这证明 `wg` 未接收 `ldflags`。

- [ ] **步骤 2：构造共用链接参数**

保留现有自增逻辑，让 `$APP_VER_FULL` 成为两个程序共同的版本变量值：

```powershell
$LDFLAGS_PARTS = @('-s', '-w')
$LDFLAGS_PARTS += "-X main.version=$APP_VER_FULL"
$LDFLAGS_PARTS += "-X `"main.BuildTime=$BuildTime`""
$LDFLAGS = $LDFLAGS_PARTS -join ' '
$WGD_VERSION = "wgctrl-go wgd $APP_VER_FULL"
```

`$WGD_VERSION` 仅用于控制台展示，不再作为 `main.version` 的注入值。

- [ ] **步骤 3：向两个构建命令传递同一组 ldflags**

将 `wg` 构建命令改为：

```powershell
& go build -buildvcs=false -trimpath -ldflags $LDFLAGS -o $WG_OUT_BIN_NAME $WG_TARGET_PACKAGE
```

保留 `wgd` 构建命令的 `-ldflags $LDFLAGS`。两次构建之间不得重新计算版本或时间。

- [ ] **步骤 4：验证固定版本注入**

运行：

```powershell
$env:APP_TAG = 'v9.8.7'
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\buildapp.ps1 -OS linux -Arch amd64
```

预期：

- 构建日志只显示一次版本 `v9.8.7` 和一次构建时间。
- `VERSION.txt` 内容不变。
- `wg`、`wgd` 构建成功。
- 两个二进制运行时显示相同构建时间。

- [ ] **步骤 5：验证自动进位且只递增一次**

在临时目录复制 `buildapp.ps1` 和内容为 `v3.0.9` 的 `VERSION.txt`，
并在 `PATH` 前放置记录参数后返回成功的临时 `go.cmd`。执行：

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\buildapp.ps1 -OS linux -Arch amd64
```

预期：

- 临时 `VERSION.txt` 变为 `v3.1.0`，而不是 `v3.1.1`。
- 临时 `go.cmd` 被调用两次。
- 两次参数均包含 `main.version=v3.1.0` 和相同的 `main.BuildTime=`。

### 任务 4：让 Bash 构建入口具备相同规则

**文件：**
- 修改：`runlinux.sh`

- [ ] **步骤 1：记录当前失败行为**

在临时目录复制当前 `runlinux.sh`，放置内容为 `v3.0.9` 的
`VERSION.txt` 和记录参数的临时 `go` 可执行文件，然后运行：

```bash
PATH="$PWD/fake-bin:$PATH" ./runlinux.sh
```

预期：当前脚本不会更新 `VERSION.txt`，构建参数也不包含
`main.version`、`main.BuildTime`。

- [ ] **步骤 2：实现版本读取与十进位进位**

在创建输出目录前增加：

```bash
VERSION_FILE="${ROOT_DIR}/VERSION.txt"
IS_BETA_VALUE="${IS_BETA:-false}"

if [[ -n "${APP_TAG:-}" ]]; then
    APP_TAG_VALUE="${APP_TAG}"
else
    VERSION_RAW="v3.0.0"
    if [[ -f "${VERSION_FILE}" ]]; then
        VERSION_RAW="$(head -n 1 "${VERSION_FILE}")"
    fi
    VERSION_RAW="${VERSION_RAW#v}"
    IFS='.' read -r MAJOR MINOR PATCH <<< "${VERSION_RAW}"
    [[ "${MAJOR}" =~ ^[0-9]+$ ]] || MAJOR=3
    [[ "${MINOR}" =~ ^[0-9]+$ ]] || MINOR=0
    [[ "${PATCH}" =~ ^[0-9]+$ ]] || PATCH=0

    PATCH=$((PATCH + 1))
    if (( PATCH > 9 )); then PATCH=0; MINOR=$((MINOR + 1)); fi
    if (( MINOR > 9 )); then MINOR=0; MAJOR=$((MAJOR + 1)); fi

    APP_TAG_VALUE="v${MAJOR}.${MINOR}.${PATCH}"
    printf '%s' "${APP_TAG_VALUE}" > "${VERSION_FILE}"
fi
```

- [ ] **步骤 3：生成一次构建时间和 Beta 版本**

```bash
BUILD_TIME="$(date '+%Y-%m-%d(%H:%M:%S)')"
if [[ "${IS_BETA_VALUE}" == "true" ]]; then
    APP_VERSION="${APP_TAG_VALUE}_B$(date '+%Y%m%d_%H%M')"
else
    APP_VERSION="${APP_TAG_VALUE}"
fi
LDFLAGS="-s -w -X main.version=${APP_VERSION} -X main.BuildTime=${BUILD_TIME}"
```

- [ ] **步骤 4：向两个构建命令传递相同 ldflags**

两个 `go build` 命令都增加：

```bash
-buildvcs=false -trimpath -ldflags "${LDFLAGS}"
```

在构建前打印版本和构建时间，便于核对。

- [ ] **步骤 5：验证 Bash 自增与覆盖规则**

使用 Git Bash 在临时目录运行两个场景：

```bash
PATH="$PWD/fake-bin:$PATH" ./runlinux.sh
APP_TAG=v9.8.7 PATH="$PWD/fake-bin:$PATH" ./runlinux.sh
```

预期：

- 第一次从 `v3.0.9` 变为 `v3.1.0`，两个构建命令使用相同值。
- 第二次使用 `v9.8.7`，且不修改 `VERSION.txt`。
- 两次构建命令均包含相同的构建时间。

### 任务 5：全量验证并提交

**文件：**
- 验证全部本次修改文件。

- [ ] **步骤 1：格式化并运行全量测试**

运行：

```powershell
gofmt -w cmd/wg/command.go cmd/wg/command_test.go cmd/wg/main.go cmd/wg/main_test.go cmd/wgd/main.go cmd/wgd/main_test.go internal/wgapi/server.go internal/wgapi/server_test.go
go test ./... -count=1
```

预期：全部测试通过。

- [ ] **步骤 2：执行真实 Linux 构建**

使用固定版本避免验证过程再次自增：

```powershell
$env:APP_TAG = 'v9.8.7'
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\buildapp.ps1 -OS linux -Arch amd64
```

再使用 Git Bash：

```bash
APP_TAG=v9.8.7 OUT_DIR=./dist/verify-linux-amd64 ./runlinux.sh
```

预期：两个入口均成功生成 `wg`、`wgd`。

- [ ] **步骤 3：验证真实二进制输出**

在 Linux 环境执行：

```bash
./wg -v
curl -s http://127.0.0.1:8080/api/v1/version
```

预期：两者版本均为 `v9.8.7`，构建时间非 `unknown`，平台为
`linux-amd64`。

- [ ] **步骤 4：检查差异**

运行：

```powershell
git diff --check
git status --short
```

预期：没有空白错误；只包含本功能文件以及进入任务前已经存在的未提交文件。

- [ ] **步骤 5：提交并推送**

精确暂存本功能文件，排除无关改动：

```powershell
git add cmd/wg/command.go cmd/wg/command_test.go cmd/wg/main.go cmd/wg/main_test.go
git add cmd/wgd/main.go cmd/wgd/main_test.go
git add internal/wgapi/server.go internal/wgapi/server_test.go
git add buildapp.ps1 runlinux.sh VERSION.txt
git commit -m "feat(构建): 注入自增版本与构建时间"
git push origin master
```
