#!/bin/bash
# test-install-flow.sh — 回归测试：安装 / 更新 / 卸载 流程
# 用法: bash test/test-install-flow.sh [--proxy https://a2hmarket.ai/github]
set -euo pipefail

PROXY="${2:-https://a2hmarket.ai/github}"
REPO="keman-ai/a2hmarket-cli"
BINARY="a2hmarket-cli"
INSTALL_SH_URL="${PROXY}/${REPO}/raw/main/install.sh"
UNINSTALL_SH_URL="${PROXY}/${REPO}/raw/main/uninstall.sh"

# ── 颜色 ────────────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; BLUE='\033[1;34m'; NC='\033[0m'
PASS=0; FAIL=0; TOTAL=0

_step() { echo -e "\n${BLUE}▶ $*${NC}"; }
_ok()   { echo -e "${GREEN}  ✅ PASS: $*${NC}"; PASS=$(( PASS + 1 )); TOTAL=$(( TOTAL + 1 )); }
_fail() { echo -e "${RED}  ❌ FAIL: $*${NC}"; FAIL=$(( FAIL + 1 )); TOTAL=$(( TOTAL + 1 )); }
_info() { echo -e "${YELLOW}  ℹ  $*${NC}"; }

# ── 找到当前安装的 binary 路径（支持 ~/.local/bin、~/bin 等） ──
find_binary() {
    for d in "$HOME/.local/bin" "$HOME/bin" "$HOME/go/bin"; do
        [[ -x "$d/$BINARY" ]] && echo "$d/$BINARY" && return
    done
    command -v "$BINARY" 2>/dev/null || echo ""
}

# ── Phase 0: 环境预检 ────────────────────────────────────────────
_step "Phase 0: 环境预检"

if command -v curl >/dev/null 2>&1; then _ok "curl 可用"; else _fail "curl 未安装"; fi
if command -v tar  >/dev/null 2>&1; then _ok "tar 可用";  else _fail "tar 未安装"; fi

# 检测代理可用性
PROXY_TAG=$(curl -fsS --connect-timeout 8 "${PROXY}/api/repos/${REPO}/releases/latest" 2>/dev/null \
    | grep -o '"tag_name":"[^"]*"' | head -1 | sed 's/"tag_name":"//;s/"$//' || true)
if [[ -n "$PROXY_TAG" ]]; then
    _ok "代理 API 可用，最新版本: ${PROXY_TAG}"
else
    _info "代理 API 不可用，将使用 GitHub redirect 兜底"
fi

# 记录安装前的版本（如果已存在）
    PRE_BIN=$(find_binary || true)
PRE_VER=""
if [[ -n "$PRE_BIN" ]]; then
    PRE_VER=$("$PRE_BIN" --version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)
        [[ -z "$PRE_VER" ]] && PRE_VER="unknown"
    _info "安装前已有版本: ${PRE_VER} (${PRE_BIN})"
else
    _info "当前环境未检测到 ${BINARY}"
fi

# ── Phase 1: 卸载（清理干净，创造空状态） ───────────────────────
_step "Phase 1: 先运行 uninstall.sh 清理环境"

# 非交互模式：通过环境变量或 echo n 跳过删除数据的问答
bash <(curl -fsSL "$UNINSTALL_SH_URL") <<< "n" 2>&1 | head -40 && true

POST_UNINSTALL_BIN=$(find_binary || true)
if [[ -z "$POST_UNINSTALL_BIN" ]]; then
    _ok "卸载后 binary 不存在于常用路径"
else
    _fail "卸载后仍能找到 binary: ${POST_UNINSTALL_BIN}"
fi

# LaunchAgent 检测（macOS）
PLIST="$HOME/Library/LaunchAgents/ai.a2hmarket.listener.plist"
if [[ "$(uname)" == "Darwin" ]]; then
    if [[ ! -f "$PLIST" ]]; then
        _ok "LaunchAgent plist 已被移除"
    else
        _fail "LaunchAgent plist 仍存在: $PLIST"
    fi
fi

# ── Phase 2: 全新安装 ────────────────────────────────────────────
_step "Phase 2: 运行 install.sh 全新安装"

bash <(curl -fsSL "$INSTALL_SH_URL") 2>&1

# 更新 PATH（安装到 ~/.local/bin 或 ~/bin）
export PATH="$HOME/.local/bin:$HOME/bin:$HOME/go/bin:$PATH"

BIN=$(find_binary || true)
if [[ -n "$BIN" && -x "$BIN" ]]; then
    _ok "binary 存在且可执行: ${BIN}"
else
    _fail "安装后找不到 binary"
    echo -e "\n${RED}致命错误，中止测试${NC}"; exit 1
fi

# ── Phase 3: 版本检查 ────────────────────────────────────────────
_step "Phase 3: 版本号检查"

# 版本号可能有 v 前缀（v1.1.5）也可能没有（1.1.5）
INSTALLED_VER=$("$BIN" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 || true)
if [[ -n "$INSTALLED_VER" ]]; then
    _ok "版本号正确注入: ${INSTALLED_VER}"
else
    RAW=$("$BIN" --version 2>/dev/null || echo "(empty)")
    _fail "版本号异常（got: ${RAW}）"
fi

# 版本与 GitHub release 一致（去掉 v 前缀后比较）
PROXY_TAG_BARE="${PROXY_TAG#v}"
if [[ -n "$PROXY_TAG_BARE" && "$INSTALLED_VER" == "$PROXY_TAG_BARE" ]]; then
    _ok "已安装版本 (${INSTALLED_VER}) 与最新 release (${PROXY_TAG}) 一致"
elif [[ -n "$PROXY_TAG_BARE" ]]; then
    _fail "版本不一致: 安装了 ${INSTALLED_VER}，最新为 ${PROXY_TAG}"
fi

# ── Phase 4: 配置检查 ────────────────────────────────────────────
_step "Phase 4: 运行时基础检查"

# --help 不报错
if "$BIN" --help >/dev/null 2>&1; then
    _ok "--help 正常退出"
else
    _fail "--help 返回错误"
fi

# 不应有 debug 噪音日志（"配置文件不存在"）
HELP_OUTPUT=$("$BIN" --help 2>&1)
if echo "$HELP_OUTPUT" | grep -q "配置文件不存在"; then
    _fail "stdout 出现 '配置文件不存在' 噪音日志"
else
    _ok "无噪音 debug 日志"
fi

# ── Phase 5: update --check-only ─────────────────────────────────
_step "Phase 5: a2hmarket-cli update --check-only"

UPDATE_OUT=$("$BIN" update --check-only 2>&1 || true)
echo "  output: $UPDATE_OUT"
if echo "$UPDATE_OUT" | grep -qE "最新版本|latest|New version|check-only"; then
    _ok "update --check-only 运行成功"
elif echo "$UPDATE_OUT" | grep -q "获取最新版本失败"; then
    _fail "update --check-only 获取版本失败: $UPDATE_OUT"
else
    _ok "update --check-only 无报错（已是最新）"
fi

# ── Phase 6: LaunchAgent / systemd 服务 ──────────────────────────
_step "Phase 6: 后台服务检查"

case "$(uname)" in
    Darwin)
        if [[ -f "$PLIST" ]]; then
            _ok "LaunchAgent plist 已安装: $PLIST"
        else
            _fail "LaunchAgent plist 未生成"
        fi
        if launchctl list | grep -q "ai.a2hmarket.listener" 2>/dev/null; then
            _ok "LaunchAgent 已加载"
        else
            _info "LaunchAgent 已安装但暂未加载（可能凭证未配置，属正常）"
        fi
        ;;
    Linux)
        UNIT="$HOME/.config/systemd/user/a2hmarket-listener.service"
        if [[ -f "$UNIT" ]]; then
            _ok "systemd unit 已安装: $UNIT"
        else
            _fail "systemd unit 未生成"
        fi
        ;;
esac

# ── Phase 7: 再次卸载验证清理干净 ────────────────────────────────
_step "Phase 7: 再次卸载，验证完全清理"

bash <(curl -fsSL "$UNINSTALL_SH_URL") <<< "n" 2>&1 | head -40 && true

POST_UNINSTALL_BIN2=$(find_binary || true)
if [[ -z "$POST_UNINSTALL_BIN2" ]]; then
    _ok "二次卸载后 binary 不存在"
else
    _fail "二次卸载后 binary 仍存在: ${POST_UNINSTALL_BIN2}"
fi

if [[ "$(uname)" == "Darwin" && ! -f "$PLIST" ]]; then
    _ok "二次卸载后 LaunchAgent plist 已移除"
fi

# ── 汇总 ─────────────────────────────────────────────────────────
echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE} 测试结果：PASS ${PASS} / FAIL ${FAIL} / TOTAL ${TOTAL}${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

if (( FAIL > 0 )); then
    exit 1
fi
