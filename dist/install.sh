#!/bin/sh
# Gofasta CLI installer script
# Usage: curl -fsSL https://raw.githubusercontent.com/gofastadev/cli/main/dist/install.sh | sh
set -e

REPO="gofastadev/cli"
INSTALL_DIR="/usr/local/bin"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest version
VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
if [ -z "$VERSION" ]; then
    echo "Failed to get latest version"
    exit 1
fi

BINARY="gofasta-${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
    BINARY="${BINARY}.exe"
fi

URL="https://github.com/${REPO}/releases/download/v${VERSION}/${BINARY}"

echo "Installing gofasta v${VERSION} (${OS}/${ARCH})..."
curl -fsSL "$URL" -o /tmp/gofasta
chmod +x /tmp/gofasta

if [ -w "$INSTALL_DIR" ]; then
    mv /tmp/gofasta "$INSTALL_DIR/gofasta"
else
    sudo mv /tmp/gofasta "$INSTALL_DIR/gofasta"
fi

echo "gofasta v${VERSION} installed to ${INSTALL_DIR}/gofasta"
echo ""
echo "Get started:"
echo "  gofasta new myapp"
echo "  cd myapp"
echo "  gofasta dev"
