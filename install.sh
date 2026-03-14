#!/bin/bash
set -e

REPO="keman-ai/a2hmarket-cli"
BINARY="a2hmarket-cli"
PKG="github.com/${REPO}/cmd/${BINARY}"

# 自建代理（最优先，最稳定）；后续 fallback 依次尝试
A2H_PROXY="https://a2hmarket.ai/github"

# ── 颜色输出 ─────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info()  { echo -e "${GREEN}[install]${NC} $*"; }
warn()  { echo -e "${YELLOW}[install]${NC} $*"; }
error() { echo -e "${RED}[install]${NC} $*" >&2; exit 1; }

# ── 1. 优先用 go install（开发者路径）────────────────────────
if command -v go >/dev/null 2>&1; then
    info "Found Go $(go version | awk '{print $3}'), installing via go install..."
    GOPROXY="https://goproxy.cn,https://proxy.golang.org,direct" go install "${PKG}@latest"

    GOBIN=$(go env GOPATH)/bin
    if [[ ":$PATH:" != *":${GOBIN}:"* ]]; then
        warn "Binary installed to ${GOBIN}/${BINARY}"
        warn "Add the following to your shell profile (~/.zshrc / ~/.bashrc):"
        warn "  export PATH=\$PATH:${GOBIN}"
        warn "Then run: source ~/.zshrc"
    else
        info "✓ ${BINARY} installed successfully → ${GOBIN}/${BINARY}"
    fi
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

# 获取最新 tag，按代理优先级依次尝试
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"

TAG=""
for api_url in \
    "${A2H_PROXY}/repos/${REPO}/releases/latest" \
    "https://ghproxy.com/${GITHUB_API}" \
    "$GITHUB_API"; do
    TAG=$(curl -fsSL --connect-timeout 8 "$api_url" 2>/dev/null \
        | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    [[ -n "$TAG" ]] && { info "Fetched tag via: $api_url"; break; }
done
[[ -z "$TAG" ]] && error "Failed to fetch latest release tag. Please check your network."

info "Latest release: ${TAG}"

# 构造下载 URL，按代理优先级依次尝试
FILENAME="${BINARY}_${OS}_${ARCH}.${ARCHIVE_EXT}"
BASE_URL="https://github.com/${REPO}/releases/download/${TAG}/${FILENAME}"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

ARCHIVE="${TMP_DIR}/${FILENAME}"

info "Downloading ${FILENAME}..."
DOWNLOADED=false
for download_url in \
    "${A2H_PROXY}/${REPO}/releases/download/${TAG}/${FILENAME}" \
    "https://ghproxy.com/${BASE_URL}" \
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

# 安装位置：优先 /usr/local/bin，回退到 ~/bin
install_to() {
    local dest="$1"
    mkdir -p "$dest"
    if mv "$BIN_SRC" "${dest}/${BINARY}" 2>/dev/null; then
        info "✓ ${BINARY} installed → ${dest}/${BINARY}"
        return 0
    fi
    return 1
}

if install_to "/usr/local/bin"; then
    :
elif sudo mv "$BIN_SRC" "/usr/local/bin/${BINARY}" 2>/dev/null; then
    info "✓ ${BINARY} installed → /usr/local/bin/${BINARY} (via sudo)"
elif install_to "$HOME/bin"; then
    if [[ ":$PATH:" != *":$HOME/bin:"* ]]; then
        warn "Add to your shell profile: export PATH=\$PATH:\$HOME/bin"
    fi
else
    # 最后兜底：装到当前目录
    mv "$BIN_SRC" "./${BINARY}"
    warn "Installed to current directory. Move it to a directory in PATH:"
    warn "  sudo mv ./${BINARY} /usr/local/bin/"
fi

echo ""
info "Run '${BINARY} --help' to get started."
