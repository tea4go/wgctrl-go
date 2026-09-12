#!/usr/bin/env bash
# 在 Linux 上构建 wgc / wgd（等价于 Windows 下的 runlinux.cmd + buildapp.ps1 -OS linux）。
#
# 关键点：
#   - 使用 CGO_ENABLED=0 交叉编译纯静态二进制，产物不依赖 glibc 版本，
#     可在老系统（如 Ubuntu 20.04 / glibc 2.31）上直接运行。
#   - 版本号沿用 buildapp.ps1 的规则：优先取环境变量 APP_TAG，否则读取
#     VERSION.txt 并自动递增 PATCH（每位最大为 9，进位时向上进位）。
#
# 用法:
#   ./runlinux.sh                 # 默认 linux/amd64
#   ./runlinux.sh -Arch arm64     # linux/arm64
#   APP_TAG=v3.1.0 ./runlinux.sh  # 强制版本号（不更新 VERSION.txt）
#   IS_BETA=true ./runlinux.sh    # 构建测试版，版本号追加 _B日期_时间 后缀
#   ./runlinux.sh --run           # 构建后本地冒烟运行 ./wgc -h
#
# 输出产物（项目根目录）:
#   wgc    —— 由 ./cmd/wg 编译
#   wgd    —— 由 ./cmd/wgd 编译

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

GOOS=linux
GOARCH=amd64
RUN_SMOKE=0

# ------------------------------------------------------------
#  可覆盖的环境变量
#   APP_TAG  —— 强制构建版本号（如 v3.0.9），跳过 VERSION.txt 自动递增
#   IS_BETA  —— "true" 表示测试版（默认 false，与 buildapp.ps1 对齐）
# ------------------------------------------------------------
IS_BETA="${IS_BETA:-false}"

usage() {
    sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
}

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case "$1" in
        -Arch|--arch)
            GOARCH="$2"; shift 2 ;;
        --run)
            RUN_SMOKE=1; shift ;;
        -h|--help)
            usage; exit 0 ;;
        *)
            echo "未知参数: $1（--help 查看用法）" >&2
            exit 2 ;;
    esac
done

case "$GOARCH" in
    amd64|arm64) ;;
    *) echo "不支持的架构: $GOARCH（可选 amd64/arm64）" >&2; exit 2 ;;
esac

if ! command -v go >/dev/null 2>&1; then
    echo "[错误] 未在 PATH 中找到 go 命令，请安装 Go 并添加到 PATH。" >&2
    exit 1
fi

# ------------------------------------------------------------
#  版本号解析（与 buildapp.ps1 逻辑对齐）
#  1) APP_TAG 已设置 -> 直接使用，跳过 VERSION.txt
#  2) 否则读取 VERSION.txt（缺省从 v3.0.0 开始），PATCH 递增并进位，写回
# ------------------------------------------------------------
VERSION_FILE="$SCRIPT_DIR/VERSION.txt"

if [[ -n "${APP_TAG:-}" ]]; then
    APP_TAG="${APP_TAG}"
    echo "[信息] 使用强制版本号 APP_TAG=$APP_TAG（不更新 VERSION.txt）"
else
    MA=3; MI=0; PA=0

    if [[ -f "$VERSION_FILE" ]]; then
        raw="$(head -n 1 "$VERSION_FILE" | tr -d '[:space:]')"
        # 去掉前导 v/V，按 . - _ 拆分出三段数字
        raw="${raw#v}"; raw="${raw#V}"
        IFS='.-_' read -ra parts <<< "$raw" || true
        [[ "${parts[0]:-}" =~ ^[0-9]+$ ]] && MA=$((10#${parts[0]}))
        [[ "${parts[1]:-}" =~ ^[0-9]+$ ]] && MI=$((10#${parts[1]}))
        [[ "${parts[2]:-}" =~ ^[0-9]+$ ]] && PA=$((10#${parts[2]}))
    fi

    PA=$((PA + 1))
    if (( PA > 9 )); then PA=0; MI=$((MI + 1)); fi
    if (( MI > 9 )); then MI=0; MA=$((MA + 1)); fi

    APP_TAG="v${MA}.${MI}.${PA}"
    printf '%s' "$APP_TAG" > "$VERSION_FILE"
fi

# 与 locale 无关的时间（BuildTime 格式与 deploy.sh 一致）
BUILD_TIME="$(date '+%Y-%m-%d(%H:%M:%S)')"

if [[ "$IS_BETA" == "true" ]]; then
    _d="$(date '+%Y%m%d')"
    _t="$(date '+%H%M')"
    APP_VER_FULL="${APP_TAG}_B${_d}_${_t}"
else
    APP_VER_FULL="$APP_TAG"
fi

echo '======================================================='
echo "项目目录    : $SCRIPT_DIR"
echo "版本文件    : $VERSION_FILE"
echo "应用版本    : $APP_VER_FULL"
echo "构建时间    : $BUILD_TIME"
echo "测试版      : $IS_BETA"
echo "目标平台    : $GOOS/$GOARCH"
echo "输出文件    : $SCRIPT_DIR/wgc"
echo "输出文件    : $SCRIPT_DIR/wgd"
echo '======================================================='

# ------------------------------------------------------------
#  注入 ldflags
#   main.version   —— wgd 的版本变量
#   main.appVer    —— wg  的版本变量（buildapp.ps1 漏注入，此处补齐）
#   main.BuildTime —— 两者的编译时间变量
#  注：-X 对目标包中不存在的符号会静默忽略，故可对两个命令共用同一组 ldflags。
# ------------------------------------------------------------
LDFLAGS="-s -w -X main.version=$APP_VER_FULL -X main.appVer=$APP_VER_FULL -X main.BuildTime=$BUILD_TIME"

echo "==> 静态编译（CGO_ENABLED=0 / $GOOS / $GOARCH）..."
CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build \
    -buildvcs=false -trimpath -ldflags "$LDFLAGS" \
    -o "$SCRIPT_DIR/wgc" ./cmd/wg

CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build \
    -buildvcs=false -trimpath -ldflags "$LDFLAGS" \
    -o "$SCRIPT_DIR/wgd" ./cmd/wgd

echo "==> 产物校验（应为 statically linked）..."
file "$SCRIPT_DIR/wgc" "$SCRIPT_DIR/wgd" | sed 's/^/    /'

if [[ "$RUN_SMOKE" -eq 1 ]]; then
    echo "==> 本地冒烟运行 ./wgc -h ..."
    "$SCRIPT_DIR/wgc" -h || true
fi

echo '======================================================='
echo '[完成] 构建成功：'
echo "  - $SCRIPT_DIR/wgc"
echo "  - $SCRIPT_DIR/wgd"
echo '======================================================='
exit 0
