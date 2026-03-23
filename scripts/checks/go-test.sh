#!/usr/bin/env bash
# go-test.sh — 运行所有单元测试
set -euo pipefail

cd "$(dirname "$0")/../.."

echo "检查: go test..."
go test ./... 2>&1
