#!/bin/sh
# 本机构建：交叉编译静态二进制 + 打镜像（绕过 Docker VM 内存不足导致的 OOM）。
# 用法：scripts/build-local.sh [tag] [goos] [goarch] [--release]
#   tag     镜像 tag，默认 hookdock:0.2.1
#   goos    linux | darwin，默认 linux
#   goarch  amd64 | arm64，默认本机架构
#   --release  用 Dockerfile.release（发布用，运行时基座 golang:1.27-alpine）
# 例：scripts/build-local.sh hookdock:0.1.0 linux arm64   （本机 / 群晖 arm64 / Mac docker）
#     scripts/build-local.sh hookdock:0.1.0 linux amd64   （腾讯云 110 等 x86 服务器）
set -eu
TAG="${1:-hookdock:0.2.1}"
GOOS_T="${2:-linux}"
GOARCH="${3:-}"
DFILE="Dockerfile.bin"
for a in "$@"; do [ "$a" = "--release" ] && DFILE="Dockerfile.release"; done

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

echo "==> 生成前端内嵌 OpenAPI 文档"
go run . -dump-openapi > build/openapi.json 2>/dev/null
sh scripts/gen-openapi-file.sh build/openapi.json || echo "  （生成失败，保留原 openapi.gen.js）"

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

echo "==> 打镜像 ${TAG}（${DFILE}，只需 ${BIN}）"
cp Dockerfile.bin.dockerignore .dockerignore
trap 'rm -f .dockerignore' EXIT INT TERM
# RUNTIME_IMAGE 走各 Dockerfile 的 ARG 默认值：Dockerfile.bin=alpine:3.20，Dockerfile.release=golang:1.27-alpine
# 必须显式 --platform：宿主是 Apple Silicon 时不传会打成 arm64 元数据，
# 交叉编译出的 amd64 二进制塞进去，manifest inspect 会显示错误架构。
docker build --platform "${GOOS_T}/${GOARCH}" -f "$DFILE" --build-arg BIN="$BIN" -t "$TAG" .
echo "==> 完成：${TAG}（${GOOS_T}/${GOARCH}）"
