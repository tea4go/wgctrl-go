#Requires -Version 5.1
[CmdletBinding()]
param(
    [ValidateSet('windows', 'linux', 'macos')]
    [string]$OS = 'windows',

    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64'
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
Clear-Host

# ============================================================
#  Paths & environment
# ============================================================
$SCRIPT_DIR = Split-Path -Parent $MyInvocation.MyCommand.Path
if (-not $SCRIPT_DIR.EndsWith('\')) { $SCRIPT_DIR += '\' }

$env:GOCACHE = Join-Path $SCRIPT_DIR '.gocache'
$env:GOTMPDIR = Join-Path $SCRIPT_DIR '.gotmp'

switch ($OS) {
    'windows' {
        $TargetGOOS = 'windows'
        $WG_OUT_BIN_NAME = 'wg.exe'
        $WGD_OUT_BIN_NAME = 'wgd.exe'
    }
    'linux' {
        $TargetGOOS = 'linux'
        $WG_OUT_BIN_NAME = 'wg'
        $WGD_OUT_BIN_NAME = 'wgd'
    }
    'macos' {
        $TargetGOOS = 'darwin'
        $WG_OUT_BIN_NAME = 'wg'
        $WGD_OUT_BIN_NAME = 'wgd'
    }
}

$WG_TARGET_PACKAGE = './cmd/wg'
$WGD_TARGET_PACKAGE = './cmd/wgd'
$WG_EXE = Join-Path $SCRIPT_DIR $WG_OUT_BIN_NAME
$RUN_EXE = Join-Path $SCRIPT_DIR $WGD_OUT_BIN_NAME

if (-not (Test-Path $env:GOCACHE))  { New-Item -ItemType Directory -Path $env:GOCACHE  -Force | Out-Null }
if (-not (Test-Path $env:GOTMPDIR)) { New-Item -ItemType Directory -Path $env:GOTMPDIR -Force | Out-Null }

# ============================================================
#  Overridable env vars (set before calling this script):
#    APP_TAG        - force build version (e.g. v3.0.9), skips VERSION.txt auto-increment
#    IS_BETA        - "true" or empty (default: empty, align with Makefile)
#    WGD_API_KEY    - optional runtime fallback REST auth key (X-API-Key request header).
#                     Not baked into the binary; wgd reads it at launch.
#                     Priority at runtime: --api-key/-x > WGD_API_KEY > auto-generated 256-bit key.
#                     If neither flag nor env is provided, wgd generates a random 256-bit key
#                     on startup and prints it to stderr + logs (auth is ALWAYS enabled).
#  Persistent version file:
#    VERSION.txt at project root (SCRIPT_DIR) holds last MAJOR.MINOR.PATCH;
#    each build auto-increments PATCH, carrying over when any digit > 9
#      e.g. v3.0.9 -> v3.1.0 ; v3.9.9 -> v4.0.0
# ============================================================
if ([string]::IsNullOrWhiteSpace($env:IS_BETA))     { $env:IS_BETA     = 'false' }

if ([string]::IsNullOrWhiteSpace($env:WGD_API_KEY)) {
    Write-Host '[提示] 环境变量 WGD_API_KEY 为空；wgd 启动时会自动生成一个 256 位随机 X-API-Key 并打印到 stderr + 日志。' -ForegroundColor Cyan
    Write-Host '       也可在启动时显式传入： .\wgd.exe --api-key "<强随机密钥>" -a 127.0.0.1:6791' -ForegroundColor Cyan
} else {
    Write-Host ('[信息] 检测到 WGD_API_KEY（长度 {0}）；运行时可再用 --api-key/-x 覆盖。' -f $env:WGD_API_KEY.Length) -ForegroundColor Cyan
}

$VERSION_FILE = Join-Path $SCRIPT_DIR 'VERSION.txt'

# ============================================================
#  Pre-flight checks
# ============================================================
if (-not (Get-Command 'go' -ErrorAction SilentlyContinue)) {
    Write-Host '[错误] 未在 PATH 中找到 go 命令，请安装 Go 并添加到系统 PATH。' -ForegroundColor Red
    exit 1
}

# ============================================================
#  Version resolution (align with Makefile logic)
#  Priority:
#    1) APP_TAG env var already set -> use it directly, skip VERSION.txt
#    2) else read VERSION.txt (or start v3.0.0 if missing),
#       increment PATCH with carry (each digit max = 9), persist back
# ============================================================
if (-not [string]::IsNullOrWhiteSpace($env:APP_TAG)) {
    $APP_TAG = $env:APP_TAG.Trim()
    Write-Host "[信息] 使用强制版本号 APP_TAG=$APP_TAG（不更新 VERSION.txt）" -ForegroundColor Cyan
} else {
    [int]$MA = 3; [int]$MI = 0; [int]$PA = 0

    if (Test-Path $VERSION_FILE) {
        $raw = (Get-Content $VERSION_FILE -TotalCount 1 -ErrorAction SilentlyContinue)
        if (-not [string]::IsNullOrWhiteSpace($raw)) {
            $cur = $raw.Trim().TrimStart('v') -replace '\s', ''
            $parts = $cur -split '[.\-_ ]'
            if ($parts.Count -ge 1 -and $parts[0] -match '^\d+$') { $MA = [int]$parts[0] }
            if ($parts.Count -ge 2 -and $parts[1] -match '^\d+$') { $MI = [int]$parts[1] }
            if ($parts.Count -ge 3 -and $parts[2] -match '^\d+$') { $PA = [int]$parts[2] }
        }
    }

    $PA++
    if ($PA -gt 9) { $PA = 0; $MI++ }
    if ($MI -gt 9) { $MI = 0; $MA++ }

    $APP_TAG = "v$MA.$MI.$PA"
    Set-Content -Path $VERSION_FILE -Value $APP_TAG -Encoding ASCII -NoNewline
}

# ============================================================
#  Locale-INDEPENDENT date/time via Get-Date -Format
#    BuildTime : "yyyy-MM-dd(HH:mm:ss)"
#    _d (date) : "yyyyMMdd"
#    _t (time) : "HHmm"
# ============================================================
$now       = Get-Date
$BuildTime = $now.ToString('yyyy-MM-dd(HH:mm:ss)')
$_d        = $now.ToString('yyyyMMdd')
$_t        = $now.ToString('HHmm')
if ($env:IS_BETA -eq 'true') {
    $APP_VER_FULL = "${APP_TAG}_B${_d}_${_t}"
} else {
    $APP_VER_FULL = $APP_TAG
}

Write-Host '======================================================='
Write-Host "项目目录    : $SCRIPT_DIR"
Write-Host "版本文件    : $VERSION_FILE"
if ($env:IS_BETA -eq 'true') {
    Write-Host ("应用版本    : {0}       (测试版=true: v3.0.1_B20060930_0930)" -f $APP_VER_FULL)
} else {
    Write-Host ("应用版本    : {0}       (测试版=false: 仅 v3.0.1)" -f $APP_VER_FULL)
}
Write-Host "构建时间    : $BuildTime"
Write-Host "测试版      : $($env:IS_BETA)"
Write-Host "目标平台    : $OS/$Arch"
Write-Host "Go 目标     : $TargetGOOS/$Arch"
Write-Host "输出文件    : $WG_EXE"
Write-Host "输出文件    : $RUN_EXE"
Write-Host '======================================================='

# ============================================================
#  Clean up & stale process cleanup (kill ONLY our own exes +
#  Go toolchain subprocesses; never kill arbitrary go.exe
#  since other Go projects / IDE backends may be running)
# ============================================================
Get-ChildItem -Path $SCRIPT_DIR -Filter '*.exe.old' -ErrorAction SilentlyContinue | Remove-Item -Force -ErrorAction SilentlyContinue

if ($OS -eq 'windows') {
    foreach ($proc in @('wgd', 'compile', 'asm', 'link')) {
        Get-Process -Name $proc -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    }
}

# ============================================================
#  Build with ldflags injection — both commands receive the same:
#    - main.version        (computed APP_VER_FULL)
#    - main.BuildTime      ("yyyy-MM-dd(HH:mm:ss)")
# ============================================================
$LDFLAGS_PARTS = @('-s', '-w')
$LDFLAGS_PARTS += "-X main.version=$APP_VER_FULL"
$LDFLAGS_PARTS += "-X `"main.BuildTime=$BuildTime`""
$LDFLAGS = $LDFLAGS_PARTS -join ' '

$env:GOOS = $TargetGOOS
$env:GOARCH = $Arch

Write-Host "执行构建: GOOS=$TargetGOOS GOARCH=$Arch go build -buildvcs=false -trimpath -ldflags `"$LDFLAGS`" -o `"$WG_OUT_BIN_NAME`" $WG_TARGET_PACKAGE"
& go build -buildvcs=false -trimpath -ldflags $LDFLAGS -o $WG_OUT_BIN_NAME $WG_TARGET_PACKAGE
if ($LASTEXITCODE -ne 0) {
    Write-Host '[错误] 构建 wg 失败。' -ForegroundColor Red
    exit $LASTEXITCODE
}
Write-Host "构建成功: $WG_EXE" -ForegroundColor Green

Write-Host "执行构建: GOOS=$TargetGOOS GOARCH=$Arch go build -buildvcs=false -trimpath -ldflags `"$LDFLAGS`" -o `"$WGD_OUT_BIN_NAME`" $WGD_TARGET_PACKAGE"
& go build -buildvcs=false -trimpath -ldflags $LDFLAGS -o $WGD_OUT_BIN_NAME $WGD_TARGET_PACKAGE
if ($LASTEXITCODE -ne 0) {
    Write-Host '[错误] 构建 wgd 失败。' -ForegroundColor Red
    exit $LASTEXITCODE
}
Write-Host "构建成功: $RUN_EXE" -ForegroundColor Green

# Optional: copy to PATH alias directory if exists
$ALIAS_DIR = 'C:\DevDisk\Other\Alias'
if ($OS -eq 'windows' -and (Test-Path $ALIAS_DIR)) {
    Copy-Item -Path $WG_EXE -Destination $ALIAS_DIR -Force -ErrorAction SilentlyContinue
    Copy-Item -Path $RUN_EXE -Destination $ALIAS_DIR -Force -ErrorAction SilentlyContinue
}

Write-Host '======================================================='
Write-Host '[完成] 全部构建成功，不自动运行输出文件。' -ForegroundColor Green
Write-Host "  - $WG_EXE"
Write-Host "  - $RUN_EXE"
Write-Host '======================================================='
exit 0
