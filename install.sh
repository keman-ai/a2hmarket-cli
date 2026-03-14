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

# ── 将安装目录写入 shell profile ──────────────────────────────
add_to_path() {
    local dir="$1"

    # 检测当前 shell
    local profile=""
    local shell_name
    shell_name=$(basename "${SHELL:-bash}")
    case "$shell_name" in
        zsh)  profile="$HOME/.zshrc" ;;
        bash) profile="${HOME}/.bashrc" ;;
        *)    profile="${HOME}/.profile" ;;
    esac

    local export_line="export PATH=\"\$PATH:${dir}\""

    # 已在 PATH 中则跳过
    if [[ ":$PATH:" == *":${dir}:"* ]]; then
        return 0
    fi

    # 已写入 profile 则跳过
    if grep -qF "$export_line" "$profile" 2>/dev/null; then
        return 0
    fi

    echo "" >> "$profile"
    echo "# a2hmarket-cli" >> "$profile"
    echo "$export_line" >> "$profile"
    info "Added to ${profile}: ${export_line}"
    warn "Run the following to apply immediately:"
    warn "  source ${profile}"
}

# ── 1. 优先用 go install（开发者路径）────────────────────────
if command -v go >/dev/null 2>&1; then
    info "Found Go $(go version | awk '{print $3}'), installing via go install..."
    GOPROXY="https://goproxy.cn,https://proxy.golang.org,direct" go install "${PKG}@latest"

    GOBIN=$(go env GOPATH)/bin
    info "✓ ${BINARY} installed → ${GOBIN}/${BINARY}"
    add_to_path "$GOBIN"
    echo ""
    info "Run '${BINARY} --help' to get started."
    exit 0
fi

# ── 2. 无 Go 环境：下载预编译二进制 ──────────────────────────
info "Go not found, downloading pre-compiled binary..."

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

# 安装位置：优先 /usr/local/bin（无需修改 PATH），回退到 ~/bin
INSTALL_DIR=""
if mv "$BIN_SRC" "/usr/local/bin/${BINARY}" 2>/dev/null; then
    INSTALL_DIR="/usr/local/bin"
    info "✓ ${BINARY} installed → /usr/local/bin/${BINARY}"
elif sudo mv "$BIN_SRC" "/usr/local/bin/${BINARY}" 2>/dev/null; then
    INSTALL_DIR="/usr/local/bin"
    info "✓ ${BINARY} installed → /usr/local/bin/${BINARY} (via sudo)"
else
    INSTALL_DIR="$HOME/bin"
    mkdir -p "$INSTALL_DIR"
    mv "$BIN_SRC" "${INSTALL_DIR}/${BINARY}"
    info "✓ ${BINARY} installed → ${INSTALL_DIR}/${BINARY}"
    add_to_path "$INSTALL_DIR"
fi

echo ""
info "Run '${BINARY} --help' to get started."
