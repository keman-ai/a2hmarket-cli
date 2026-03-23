#!/usr/bin/env bash
# schema-check.sh — 检查 outbox 表的完整性（store + dispatcher + listener flush）
set -euo pipefail

cd "$(dirname "$0")/../.."

echo "检查: outbox 完整性..."

ERRORS=0

# 检查每张 outbox 表是否在 schema.go 中定义、在 dispatcher 中有 flush、在 listener.go 中有 flush 调用
for table in a2a_outbox push_outbox media_outbox; do
    # 检查 schema
    if ! grep -q "CREATE TABLE IF NOT EXISTS $table" internal/store/schema.go 2>/dev/null; then
        echo "  [缺失] $table 未在 internal/store/schema.go 中定义"
        ERRORS=$((ERRORS + 1))
    fi

    # 检查 dispatcher flush 文件存在
    dispatcher_file="internal/dispatcher/${table}.go"
    if [ ! -f "$dispatcher_file" ]; then
        echo "  [缺失] $table 的 dispatcher flush 文件: $dispatcher_file"
        ERRORS=$((ERRORS + 1))
    fi
done

# 检查 listener.go 是否调用了三个 flush
for flush_fn in FlushA2AOutbox FlushPushOutbox FlushMediaOutbox; do
    if ! grep -q "$flush_fn" cmd/a2hmarket-cli/listener.go 2>/dev/null; then
        echo "  [缺失] listener.go 未调用 $flush_fn"
        ERRORS=$((ERRORS + 1))
    fi
done

if [ "$ERRORS" -gt 0 ]; then
    echo "  发现 $ERRORS 个 outbox 完整性问题"
    exit 1
fi

echo "  outbox 完整性检查通过"
