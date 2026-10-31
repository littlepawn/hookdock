#!/bin/sh
# 本机构建：交叉编译静态二进制 + 打镜像（绕过 Docker VM 内存不足导致的 OOM）。
# 用法：scripts/build-local.sh [tag] [goos] [goarch]
#   tag     镜像 tag，默认 hookdock:0.1.0
#   goos    linux | darwin，默认 linux
#   goarch  amd64 | arm64，默认本机架构
# 例：scripts/build-local.sh hookdock:0.1.0 linux arm64   （本机 / 群晖 arm64 / Mac docker）
#     scripts/build-local.sh hookdock:0.1.0 linux amd64   （腾讯云 110 等 x86 服务器）
set -eu
TAG="${1:-hookdock:0.1.0}"
GOOS_T="${2:-linux}"
GOARCH="${3:-}"

cd "$(dirname "$0")/.."

if [ -z "$GOARCH" ]; then
  case "$(uname -m)" in
    arm64|aarch64) GOARCH=arm64 ;;
    x86_64|amd64)  GOARCH=amd64 ;;
    *) echo "unsupported host arch: $(uname -m)" >&2; exit 1 ;;
  esac
fi

BIN="build/hookdock-$GOOS_T-$GOARCH"
mkdir -p build

echo "==> 前端构建"
( cd web && bun install --silent >/dev/null 2>&1; bun run build )

echo "==> 交叉编译 ${GOOS_T}/${GOARCH}（静态、无 CGO）"
CGO_ENABLED=0 GOOS=$GOOS_T GOARCH=$GOARCH GOFLAGS=-p=2 \
  go build -trimpath -ldflags "-s -w" -o "$BIN" .
ls -lh "$BIN"

if [ "$GOOS_T" = "darwin" ]; then
  echo "==> macOS 原生二进制：$BIN（直接用 ./$BIN 跑，不进 docker）"
  exit 0
fi

echo "==> 打镜像 ${TAG}（Dockerfile.bin，只需 ${BIN}）"
cp Dockerfile.bin.dockerignore .dockerignore
trap 'rm -f .dockerignore' EXIT INT TERM
docker build -f Dockerfile.bin --build-arg BIN="$BIN" -t "$TAG" .
echo "==> 完成：${TAG}（${GOOS_T}/${GOARCH}）"
