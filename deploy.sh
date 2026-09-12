#!/usr/bin/env bash
# 将 wg / wgd / wg-quick 静态编译并部署到远程 Linux 主机的 /opt/wireguard。
#
# 关键点：使用 CGO_ENABLED=0 交叉编译纯静态二进制，产物不依赖 glibc 版本，
# 因此能在老系统（如 Ubuntu 20.04 / glibc 2.31）上直接运行，规避
# 「本地动态链接二进制因 glibc 版本不兼容而无法在远程启动」的问题。
#
# 用法:
#   ./deploy.sh                                  # 默认 tony@192.168.193.78:6443 -> /opt/wireguard
#   ./deploy.sh --host 1.2.3.4 --user root       # 覆盖主机与用户
#   ./deploy.sh --port 2222                      # 覆盖 SSH 端口
#   ./deploy.sh --dir /srv/wireguard             # 覆盖安装目录
#   ./deploy.sh --help                           # 查看本帮助
#
# 远程连接信息也可用同名环境变量覆盖:
#   REMOTE_HOST / REMOTE_PORT / REMOTE_USER / INSTALL_DIR

set -euo pipefail

# 默认远程连接信息
REMOTE_USER="${REMOTE_USER:-tony}"
REMOTE_HOST="${REMOTE_HOST:-192.168.193.78}"
REMOTE_PORT="${REMOTE_PORT:-6443}"
INSTALL_DIR="${INSTALL_DIR:-/opt/wireguard}"

# 打印脚本开头注释块作为帮助文本（去除行首 "# " 前缀）
usage() {
  awk 'NR > 1 { if (/^#/) { sub(/^# ?/, ""); print } else { exit } }' "$0"
}

# 解析命令行参数（覆盖默认值）
while [[ $# -gt 0 ]]; do
  case "$1" in
    --host) REMOTE_HOST="$2"; shift 2 ;;
    --port) REMOTE_PORT="$2"; shift 2 ;;
    --user) REMOTE_USER="$2"; shift 2 ;;
    --dir)  INSTALL_DIR="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "未知参数: $1（--help 查看用法）" >&2; exit 2 ;;
  esac
done

TARGET="${REMOTE_USER}@${REMOTE_HOST}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 临时构建目录，脚本退出时自动清理
BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "$BUILD_DIR"' EXIT

BUILD_TIME="$(date '+%Y-%m-%d(%H:%M:%S)')"
# wg / wgd 的 main 包存在 BuildTime 变量；wg-quick 无，只做精简与去符号。
LDFLAGS="-s -w -X main.BuildTime=$BUILD_TIME"

echo "==> 静态编译（CGO_ENABLED=0 / linux / amd64）..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$BUILD_DIR/wg"       ./cmd/wg
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$BUILD_DIR/wgd"      ./cmd/wgd
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w"    -o "$BUILD_DIR/wg-quick" ./cmd/wg-quick

echo "==> 产物校验（应为 statically linked）..."
file "$BUILD_DIR"/wg "$BUILD_DIR"/wgd "$BUILD_DIR"/wg-quick | sed 's/^/    /'

echo "==> 部署到 $TARGET:$REMOTE_PORT  $INSTALL_DIR/"
ssh -p "$REMOTE_PORT" "$TARGET" "mkdir -p '$INSTALL_DIR'"
scp -P "$REMOTE_PORT" "$BUILD_DIR"/wg "$BUILD_DIR"/wgd "$BUILD_DIR"/wg-quick "$TARGET:$INSTALL_DIR/"

echo "==> 远程验证..."
ssh -p "$REMOTE_PORT" "$TARGET" "INSTALL_DIR='$INSTALL_DIR' bash -s" <<'REMOTE'
cd "$INSTALL_DIR" || exit 1
echo "    wg       -> $(timeout 15 ./wg -v 2>&1 | tail -1)"
echo "    wgd      -> $(timeout 15 ./wgd -v 2>&1 | tail -1)"
echo "    wg-quick -> $(timeout 15 ./wg-quick 2>&1 | head -1)"
REMOTE

echo "==> 完成。"
