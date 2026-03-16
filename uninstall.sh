#!/bin/bash
# uninstall.sh — 彻底卸载 a2hmarket-cli
set -e

BINARY="a2hmarket-cli"
CONFIG_DIR="$HOME/.a2hmarket"

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[uninstall]${NC} $*"; }
warn()  { echo -e "${YELLOW}[uninstall]${NC} $*"; }
done_() { echo -e "${GREEN}[uninstall]${NC} ✓ $*"; }

# ── 1. 停止并移除 macOS LaunchAgent ─────────────────────────────
remove_launchagent() {
    local plist="$HOME/Library/LaunchAgents/ai.a2hmarket.listener.plist"
    if [[ -f "$plist" ]]; then
        launchctl unload "$plist" 2>/dev/null || true
        rm -f "$plist"
        done_ "LaunchAgent removed: $plist"
    fi
}

# ── 2. 停止并移除 Linux systemd user service ────────────────────
remove_systemd() {
    local unit="$HOME/.config/systemd/user/a2hmarket-listener.service"
    if command -v systemctl >/dev/null 2>&1; then
        systemctl --user stop  a2hmarket-listener.service 2>/dev/null || true
        systemctl --user disable a2hmarket-listener.service 2>/dev/null || true
        systemctl --user daemon-reload 2>/dev/null || true
    fi
    if [[ -f "$unit" ]]; then
        rm -f "$unit"
        done_ "Systemd unit removed: $unit"
    fi
}

# ── 3. 终止残留进程 ──────────────────────────────────────────────
kill_processes() {
    if pgrep -f "${BINARY} listener" >/dev/null 2>&1; then
        pkill -f "${BINARY} listener" 2>/dev/null || true
        sleep 1
        done_ "Listener process stopped."
    fi
}

# ── 4. 删除二进制文件 ────────────────────────────────────────────
remove_binary() {
    local removed=false
    for dir in /usr/local/bin "$HOME/.local/bin" "$HOME/bin" "$HOME/go/bin" "$(go env GOPATH 2>/dev/null)/bin"; do
        local bin="$dir/$BINARY"
        if [[ -f "$bin" ]]; then
            rm -f "$bin" && done_ "Binary removed: $bin" && removed=true
        fi
    done
    $removed || warn "Binary not found in common locations (already removed or in custom PATH)."
}

# ── 5. 清理 shell profile 中的 PATH 条目 ────────────────────────
clean_path_entries() {
    local profiles=("$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.profile")
    for profile in "${profiles[@]}"; do
        if [[ -f "$profile" ]] && grep -q "a2hmarket-cli" "$profile" 2>/dev/null; then
            # 删除 a2hmarket-cli 注释行及紧随其后的 export PATH 行
            sed -i.bak '/# a2hmarket-cli/{N;d}' "$profile" 2>/dev/null || \
            sed -i '' '/# a2hmarket-cli/{N;d}' "$profile" 2>/dev/null || true
            rm -f "${profile}.bak"
            done_ "PATH entry removed from $profile"
        fi
    done
}

# ── 6. 可选：删除配置和数据目录 ─────────────────────────────────
remove_data() {
    if [[ -d "$CONFIG_DIR" ]]; then
        echo ""
        echo -e "${YELLOW}Config and data directory found: ${CONFIG_DIR}${NC}"
        echo "  Contains: credentials, local SQLite DB, logs, cache"
        read -r -p "Delete all data in $CONFIG_DIR? [y/N] " answer
        case "$answer" in
            [yY][eE][sS]|[yY])
                rm -rf "$CONFIG_DIR"
                done_ "Data directory deleted: $CONFIG_DIR"
                ;;
            *)
                warn "Skipped. Remove manually if needed: rm -rf $CONFIG_DIR"
                ;;
        esac
    fi
}

# ── 主流程 ──────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN} Uninstalling a2hmarket-cli${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

kill_processes

case "$(uname -s)" in
    Darwin) remove_launchagent ;;
    Linux)  remove_systemd ;;
esac

remove_binary
clean_path_entries
remove_data

echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN} Uninstall complete.${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
