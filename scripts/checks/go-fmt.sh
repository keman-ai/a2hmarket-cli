#!/usr/bin/env bash
# go-fmt.sh — 检查代码格式化
set -euo pipefail

cd "$(dirname "$0")/../.."

echo "检查: go fmt..."
UNFORMATTED=$(gofmt -l . 2>/dev/null || true)

if [ -n "$UNFORMATTED" ]; then
    echo "以下文件需要格式化:"
    echo "$UNFORMATTED"
    echo ""
    echo "运行 'go fmt ./...' 修复。"
    exit 1
fi
