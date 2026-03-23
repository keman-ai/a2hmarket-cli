#!/usr/bin/env bash
# go-build.sh — 验证项目可以正常编译
set -euo pipefail

cd "$(dirname "$0")/../.."

echo "检查: go build..."
CGO_ENABLED=0 go build -o /dev/null ./cmd/a2hmarket-cli 2>&1
