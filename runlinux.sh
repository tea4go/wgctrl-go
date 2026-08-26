#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GOOS_VALUE="${GOOS:-linux}"
GOARCH_VALUE="${GOARCH:-amd64}"
CGO_ENABLED_VALUE="${CGO_ENABLED:-0}"
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/dist/${GOOS_VALUE}-${GOARCH_VALUE}}"
VERSION_FILE="${ROOT_DIR}/VERSION.txt"
IS_BETA_VALUE="${IS_BETA:-false}"

mkdir -p "${OUT_DIR}"

APP_TAG_VALUE="${APP_TAG:-}"
APP_TAG_VALUE="${APP_TAG_VALUE#"${APP_TAG_VALUE%%[![:space:]]*}"}"
APP_TAG_VALUE="${APP_TAG_VALUE%"${APP_TAG_VALUE##*[![:space:]]}"}"

if [[ -z "${APP_TAG_VALUE}" ]]; then
    VERSION_RAW="v3.0.0"
    if [[ -f "${VERSION_FILE}" ]]; then
        VERSION_RAW="$(head -n 1 "${VERSION_FILE}")"
    fi
    VERSION_RAW="${VERSION_RAW#v}"
    IFS='.' read -r MAJOR MINOR PATCH <<< "${VERSION_RAW}"
    [[ "${MAJOR:-}" =~ ^[0-9]+$ ]] || MAJOR=3
    [[ "${MINOR:-}" =~ ^[0-9]+$ ]] || MINOR=0
    [[ "${PATCH:-}" =~ ^[0-9]+$ ]] || PATCH=0

    PATCH=$((PATCH + 1))
    if (( PATCH > 9 )); then
        PATCH=0
        MINOR=$((MINOR + 1))
    fi
    if (( MINOR > 9 )); then
        MINOR=0
        MAJOR=$((MAJOR + 1))
    fi

    APP_TAG_VALUE="v${MAJOR}.${MINOR}.${PATCH}"
    printf '%s' "${APP_TAG_VALUE}" > "${VERSION_FILE}"
fi

BUILD_TIME="$(date '+%Y-%m-%d(%H:%M:%S)')"
if [[ "${IS_BETA_VALUE}" == "true" ]]; then
    APP_VERSION="${APP_TAG_VALUE}_B$(date '+%Y%m%d_%H%M')"
else
    APP_VERSION="${APP_TAG_VALUE}"
fi
LDFLAGS="-s -w -X main.version=${APP_VERSION} -X main.BuildTime=${BUILD_TIME}"

echo "Version: ${APP_VERSION}"
echo "Build time: ${BUILD_TIME}"
echo "Building wg for ${GOOS_VALUE}/${GOARCH_VALUE} -> ${OUT_DIR}/wg"
env CGO_ENABLED="${CGO_ENABLED_VALUE}" GOOS="${GOOS_VALUE}" GOARCH="${GOARCH_VALUE}" \
    go build -buildvcs=false -trimpath -ldflags "${LDFLAGS}" -o "${OUT_DIR}/wg" ./cmd/wg

echo "Building wgd for ${GOOS_VALUE}/${GOARCH_VALUE} -> ${OUT_DIR}/wgd"
env CGO_ENABLED="${CGO_ENABLED_VALUE}" GOOS="${GOOS_VALUE}" GOARCH="${GOARCH_VALUE}" \
    go build -buildvcs=false -trimpath -ldflags "${LDFLAGS}" -o "${OUT_DIR}/wgd" ./cmd/wgd

echo "Build complete: ${OUT_DIR}"
