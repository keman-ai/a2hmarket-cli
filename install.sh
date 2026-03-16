#!/bin/bash
set -e

REPO="keman-ai/a2hmarket-cli"
BINARY="a2hmarket-cli"
PKG="github.com/${REPO}/cmd/${BINARY}"

# 自建 GitHub 代理（国内加速）
A2H_PROXY="https://a2hmarket.ai/github"

# ── 颜色输出 ─────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[install]${NC} $*"; }
warn()  { echo -e "${YELLOW}[install]${NC} $*"; }
error() { echo -e "${RED}[install]${NC} $*" >&2; exit 1; }

# ── 安装前：先停掉托管服务再杀进程，避免 launchd/systemd 立刻拉起旧进程 ─
stop_listener_before_install() {
    case "$(uname -s)" in
        Darwin)
            local plist="$HOME/Library/LaunchAgents/ai.a2hmarket.listener.plist"
            if [[ -f "$plist" ]]; then
                launchctl unload "$plist" 2>/dev/null && info "Unloaded LaunchAgent (old listener stopped)." || true
            fi
            ;;
        Linux)
            if command -v systemctl >/dev/null 2>&1; then
                systemctl --user stop a2hmarket-listener.service 2>/dev/null && info "Stopped systemd user service (old listener)." || true
            fi
            ;;
    esac
    # 再杀残留进程（非托管启动的旧进程或旧名 a2hmarket）
    if pgrep -f "${BINARY} listener run" >/dev/null 2>&1; then
        pkill -f "${BINARY} listener run" 2>/dev/null || true
        sleep 1
    fi
    if pgrep -x "a2hmarket" >/dev/null 2>&1; then
        pkill -x "a2hmarket" 2>/dev/null || true
        info "Stopped legacy 'a2hmarket' process."
    fi
}

# ── 将安装目录写入 shell profile ──────────────────────────────
# 全局变量：记录是否需要提示用户 source
NEED_SOURCE_PROFILE=""
SOURCE_PROFILE_PATH=""

add_to_path() {
    local dir="$1"

    # 检测当前 shell
    local shell_name
    shell_name=$(basename "${SHELL:-bash}")
    case "$shell_name" in
        zsh)  SOURCE_PROFILE_PATH="$HOME/.zshrc" ;;
        bash) SOURCE_PROFILE_PATH="${HOME}/.bashrc" ;;
        *)    SOURCE_PROFILE_PATH="${HOME}/.profile" ;;
    esac

    local export_line="export PATH=\"\$PATH:${dir}\""

    # 已在 PATH 中也打印提示（curl|bash 子进程里 PATH 可能不完整）
    NEED_SOURCE_PROFILE="$dir"

    # 已写入 profile 则跳过写入，但仍需提示 source
    if grep -qF "${dir}" "$SOURCE_PROFILE_PATH" 2>/dev/null; then
        return 0
    fi

    echo "" >> "$SOURCE_PROFILE_PATH"
    echo "# a2hmarket-cli" >> "$SOURCE_PROFILE_PATH"
    echo "$export_line" >> "$SOURCE_PROFILE_PATH"
    info "PATH written to ${SOURCE_PROFILE_PATH}"
}

# ── 安装 listener 为系统/用户服务（macOS launchd / Linux systemd）────────
# 传入：二进制绝对路径、配置目录。无凭证时仍写入 unit，不自动 start，避免反复报错。
install_listener_service() {
    local bin="$1"
    local config_dir="${2:-$HOME/.a2hmarket}"
    local log_path="$config_dir/store/listener.log"
    [[ -z "$bin" || ! -x "$bin" ]] && return 0
    mkdir -p "$config_dir/store"

    case "$(uname -s)" in
        Darwin)
            local plist_dir="$HOME/Library/LaunchAgents"
            local plist_path="$plist_dir/ai.a2hmarket.listener.plist"
            mkdir -p "$plist_dir"
            cat > "$plist_path" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>ai.a2hmarket.listener</string>
    <key>ProgramArguments</key>
    <array>
        <string>$bin</string>
        <string>listener</string>
        <string>run</string>
        <string>--config-dir</string>
        <string>$config_dir</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>$log_path</string>
    <key>StandardErrorPath</key>
    <string>$log_path</string>
</dict>
</plist>
EOF
            info "LaunchAgent installed: $plist_path"
            if [[ -f "$config_dir/credentials.json" ]]; then
                launchctl unload "$plist_path" 2>/dev/null || true
                launchctl load "$plist_path" 2>/dev/null && info "Listener service loaded (log: $log_path)." || warn "launchctl load failed; run: launchctl load $plist_path"
            else
                warn "No credentials yet. After '${BINARY} get-auth', run: launchctl load $plist_path"
            fi
            ;;
        Linux)
            local unit_dir="$HOME/.config/systemd/user"
            local unit_path="$unit_dir/a2hmarket-listener.service"
            mkdir -p "$unit_dir"
            cat > "$unit_path" << EOF
[Unit]
Description=a2hmarket-cli listener
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=\"$bin\" listener run --config-dir \"$config_dir\"
Restart=on-failure
RestartSec=5
StandardOutput=append:$log_path
StandardError=append:$log_path

[Install]
WantedBy=default.target
EOF
            info "Systemd user unit installed: $unit_path"
            if command -v systemctl >/dev/null 2>&1; then
                systemctl --user daemon-reload 2>/dev/null || true
                if [[ -f "$config_dir/credentials.json" ]]; then
                    systemctl --user enable --now a2hmarket-listener.service 2>/dev/null && info "Listener service enabled and started (log: $log_path)." || warn "systemctl --user start failed; run: systemctl --user enable --now a2hmarket-listener.service"
                else
                    warn "No credentials yet. After '${BINARY} get-auth', run: systemctl --user enable --now a2hmarket-listener.service"
                fi
                info "To run listener at boot without login: loginctl enable-linger \$USER"
            else
                warn "systemctl not found; start manually: $bin listener run --config-dir $config_dir"
            fi
            # 可选：若使用 supervisord，可 include 此片段
            cat > "$config_dir/supervisord-listener.conf.sample" << SUPEOF
; Include in supervisord.conf (e.g. under [include] files=...)
[program:a2hmarket-listener]
command=$bin listener run --config-dir $config_dir
directory=$HOME
autostart=true
autorestart=true
stdout_logfile=$log_path
stderr_logfile=$log_path
SUPEOF
            info "Supervisord sample: $config_dir/supervisord-listener.conf.sample"
            ;;
        *)
            return 0
            ;;
    esac
}

# ── 安装完成后的提示 ─────────────────────────────────────────
print_next_steps() {
    local dir="$1"
    echo ""
    if [[ -n "$NEED_SOURCE_PROFILE" ]]; then
        echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo -e "${GREEN} Installation complete!${NC}"
        echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo ""
        warn "To use ${BINARY} in this terminal, run:"
        echo ""
        echo "    export PATH=\"\$PATH:${dir}\""
        echo ""
        if [[ -n "$SOURCE_PROFILE_PATH" ]]; then
            warn "Or open a new terminal (PATH is already saved to ${SOURCE_PROFILE_PATH})."
        fi
    else
        echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo -e "${GREEN} Installation complete!${NC}"
        echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    fi
    echo ""
    info "Run '${BINARY} --help' to get started."
}

# 安装前先停掉托管服务并杀旧进程，再装新二进制、写新 unit 并 load/start
stop_listener_before_install

# ── 下载预编译二进制（优先 A2H_PROXY，回退 GitHub 直连）──────
# go install 依赖 GOPROXY，国内不稳定；预编译二进制走自建代理更可靠。
info "Downloading pre-compiled binary..."

# 探测系统平台
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)          ARCH="amd64" ;;
    aarch64|arm64)   ARCH="arm64" ;;
    *)               error "Unsupported architecture: $ARCH" ;;
esac
case "$OS" in
    linux|darwin) ;;
    msys*|cygwin*|mingw*) OS="windows" ;;
    *) error "Unsupported OS: $OS" ;;
esac

ARCHIVE_EXT="tar.gz"
[[ "$OS" == "windows" ]] && ARCHIVE_EXT="zip"

info "Detected platform: ${OS}/${ARCH}"

# 获取最新 tag，先尝试自建代理，再直连
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"

TAG=""
for api_url in \
    "${A2H_PROXY}/repos/${REPO}/releases/latest" \
    "$GITHUB_API"; do
    TAG=$(curl -fsSL --connect-timeout 8 "$api_url" 2>/dev/null \
        | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    [[ -n "$TAG" ]] && { info "Fetched tag via: $api_url"; break; }
done
[[ -z "$TAG" ]] && error "Failed to fetch latest release tag. Please check your network."

info "Latest release: ${TAG}"

# 构造下载 URL，先尝试自建代理，再直连 GitHub
FILENAME="${BINARY}_${OS}_${ARCH}.${ARCHIVE_EXT}"
BASE_URL="https://github.com/${REPO}/releases/download/${TAG}/${FILENAME}"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

ARCHIVE="${TMP_DIR}/${FILENAME}"

info "Downloading ${FILENAME}..."
DOWNLOADED=false
for download_url in \
    "${A2H_PROXY}/${REPO}/releases/download/${TAG}/${FILENAME}" \
    "$BASE_URL"; do
    if curl -fsSL --connect-timeout 15 -o "$ARCHIVE" "$download_url" 2>/dev/null; then
        info "Downloaded via: $download_url"
        DOWNLOADED=true
        break
    fi
    warn "Failed: $download_url"
done
$DOWNLOADED || error "All download sources failed. Please visit: https://github.com/${REPO}/releases"

# 解压
info "Extracting..."
if [[ "$ARCHIVE_EXT" == "zip" ]]; then
    command -v unzip >/dev/null 2>&1 || error "unzip not found"
    unzip -q "$ARCHIVE" -d "$TMP_DIR"
else
    tar -xzf "$ARCHIVE" -C "$TMP_DIR"
fi

BIN_SRC="${TMP_DIR}/${BINARY}"
[[ -f "$BIN_SRC" ]] || error "Binary not found in archive"
chmod +x "$BIN_SRC"

# 安装到用户目录，不需要任何系统权限
# 优先 ~/.local/bin（XDG 标准），其次 ~/bin
INSTALL_DIR="$HOME/.local/bin"
if [[ ! -d "$INSTALL_DIR" ]]; then
    INSTALL_DIR="$HOME/bin"
fi
mkdir -p "$INSTALL_DIR"
mv "$BIN_SRC" "${INSTALL_DIR}/${BINARY}"
info "✓ ${BINARY} installed → ${INSTALL_DIR}/${BINARY}"
add_to_path "$INSTALL_DIR"

print_next_steps "$INSTALL_DIR"
install_listener_service "${INSTALL_DIR}/${BINARY}" "$HOME/.a2hmarket"
