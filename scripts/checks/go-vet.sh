#!/usr/bin/env bash
# go-vet.sh — 运行 go vet 静态分析
set -euo pipefail

cd "$(dirname "$0")/../.."

echo "检查: go vet..."
go vet ./... 2>&1
