#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GOOS_VALUE="${GOOS:-linux}"
GOARCH_VALUE="${GOARCH:-amd64}"
CGO_ENABLED_VALUE="${CGO_ENABLED:-0}"
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/dist/${GOOS_VALUE}-${GOARCH_VALUE}}"

mkdir -p "${OUT_DIR}"

echo "Building wg for ${GOOS_VALUE}/${GOARCH_VALUE} -> ${OUT_DIR}/wg"
env CGO_ENABLED="${CGO_ENABLED_VALUE}" GOOS="${GOOS_VALUE}" GOARCH="${GOARCH_VALUE}" \
    go build -o "${OUT_DIR}/wg" ./cmd/wg

echo "Building wgd for ${GOOS_VALUE}/${GOARCH_VALUE} -> ${OUT_DIR}/wgd"
env CGO_ENABLED="${CGO_ENABLED_VALUE}" GOOS="${GOOS_VALUE}" GOARCH="${GOARCH_VALUE}" \
    go build -o "${OUT_DIR}/wgd" ./cmd/wgd

echo "Build complete: ${OUT_DIR}"
