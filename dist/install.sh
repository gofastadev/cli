#!/bin/sh
# Gofasta CLI installer script
# Usage: curl -fsSL https://raw.githubusercontent.com/gofastadev/cli/main/dist/install.sh | sh
# Override install directory: GOFASTA_INSTALL_DIR=/custom/path sh install.sh
set -e

REPO="gofastadev/cli"
INSTALL_DIR="${GOFASTA_INSTALL_DIR:-/usr/local/bin}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    mingw*|msys*|cygwin*) OS="windows" ;;
esac
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

# Verify the install directory is on PATH — if not, the user will get
# "command not found" after install and blame us. Warn them before they try.
case ":${PATH}:" in
    *":${INSTALL_DIR}:"*)
        # On PATH. Double-check the shell actually resolves `gofasta` to our
        # install, not a stale binary from somewhere else.
        RESOLVED=$(command -v gofasta 2>/dev/null || true)
        if [ -z "$RESOLVED" ]; then
            echo "⚠  ${INSTALL_DIR} is on \$PATH but your shell hasn't picked up"
            echo "   the new binary yet. Run 'hash -r' (bash/zsh) or open a new"
            echo "   terminal, then try 'gofasta --version'."
            echo ""
        elif [ "$RESOLVED" != "${INSTALL_DIR}/gofasta" ]; then
            echo "⚠  Your shell resolves 'gofasta' to $RESOLVED, not the copy"
            echo "   we just installed at ${INSTALL_DIR}/gofasta. Another"
            echo "   installation may be earlier on \$PATH. Run"
            echo "   'which -a gofasta' to see all of them."
            echo ""
        fi
        ;;
    *)
        echo "⚠  ${INSTALL_DIR} is not on your \$PATH. Running 'gofasta' now"
        echo "   will fail with 'command not found'. Add it to your shell config:"
        echo ""
        # Detect shell from $SHELL env var and suggest the right rc file.
        SHELL_NAME=$(basename "${SHELL:-}")
        case "$SHELL_NAME" in
            zsh)
                echo "   echo 'export PATH=\"\$PATH:${INSTALL_DIR}\"' >> ~/.zshrc"
                echo "   source ~/.zshrc"
                ;;
            bash)
                echo "   echo 'export PATH=\"\$PATH:${INSTALL_DIR}\"' >> ~/.bashrc"
                echo "   source ~/.bashrc"
                ;;
            fish)
                echo "   fish_add_path ${INSTALL_DIR}"
                ;;
            *)
                echo "   export PATH=\"\$PATH:${INSTALL_DIR}\""
                echo "   (add the line above to your shell's startup file)"
                ;;
        esac
        echo ""
        ;;
esac

echo "Get started:"
echo "  gofasta new myapp"
echo "  cd myapp"
echo "  gofasta dev"
