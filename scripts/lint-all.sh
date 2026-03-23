#!/usr/bin/env bash
# lint-all.sh — 运行所有代码质量检查
# 用法: ./scripts/lint-all.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_ROOT"

PASS=0
FAIL=0
SKIP=0

run_check() {
    local name="$1"
    local script="$2"

    if [ ! -f "$script" ]; then
        echo "  [SKIP] $name — 脚本不存在: $script"
        SKIP=$((SKIP + 1))
        return
    fi

    if bash "$script"; then
        echo "  [PASS] $name"
        PASS=$((PASS + 1))
    else
        echo "  [FAIL] $name"
        FAIL=$((FAIL + 1))
    fi
}

echo "=== a2hmarket-cli 代码质量检查 ==="
echo ""

run_check "go-build"     "$SCRIPT_DIR/checks/go-build.sh"
run_check "go-vet"       "$SCRIPT_DIR/checks/go-vet.sh"
run_check "go-fmt"       "$SCRIPT_DIR/checks/go-fmt.sh"
run_check "go-test"      "$SCRIPT_DIR/checks/go-test.sh"
run_check "schema-check" "$SCRIPT_DIR/checks/schema-check.sh"

echo ""
echo "=== 结果汇总 ==="
echo "  通过: $PASS"
echo "  失败: $FAIL"
echo "  跳过: $SKIP"

if [ "$FAIL" -gt 0 ]; then
    echo ""
    echo "存在失败的检查项，请修复后重试。"
    exit 1
fi

echo ""
echo "所有检查通过。"
