#!/bin/sh
# Gofasta CLI installer script.
#
# Usage:
#     curl -fsSL https://raw.githubusercontent.com/gofastadev/cli/main/dist/install.sh | sh
#
# Override install directory:
#     curl -fsSL https://raw.githubusercontent.com/gofastadev/cli/main/dist/install.sh | GOFASTA_INSTALL_DIR=/custom/path sh
#
# Pin a specific version:
#     curl -fsSL https://raw.githubusercontent.com/gofastadev/cli/main/dist/install.sh | GOFASTA_VERSION=v0.2.0 sh
#
# The script downloads the GoReleaser archive matching your OS/arch, verifies
# its SHA-256 against the release's checksums.txt, extracts the binary, and
# installs it. Tampered or corrupted downloads are rejected.

set -e

REPO="gofastadev/cli"
INSTALL_DIR="${GOFASTA_INSTALL_DIR:-/usr/local/bin}"
TMP_DIR=$(mktemp -d 2>/dev/null || mktemp -d -t gofasta)
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT INT TERM

# --- Detect OS and architecture --------------------------------------------

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux|darwin) ;;
    mingw*|msys*|cygwin*)
        echo "Windows is not supported by this shell installer."
        echo "Download the .zip archive directly from:"
        echo "  https://github.com/${REPO}/releases/latest"
        exit 1 ;;
    *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
    x86_64)        ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# --- Resolve version -------------------------------------------------------

if [ -n "${GOFASTA_VERSION:-}" ]; then
    # User-pinned version. Strip a leading 'v' so the rest of the script can
    # rebuild URLs without doubling it.
    VERSION="${GOFASTA_VERSION#v}"
else
    # Resolve the latest release tag from the GitHub API.
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' \
        | sed -E 's/.*"v([^"]+)".*/\1/')
    if [ -z "$VERSION" ]; then
        echo "Failed to resolve the latest gofasta release."
        echo "Try pinning a version explicitly, e.g.:"
        echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/dist/install.sh | GOFASTA_VERSION=v0.2.0 sh"
        exit 1
    fi
fi

# --- Build asset URLs (GoReleaser default archive layout) ------------------

ARCHIVE_NAME="gofasta_${VERSION}_${OS}_${ARCH}.tar.gz"
ARCHIVE_URL="https://github.com/${REPO}/releases/download/v${VERSION}/${ARCHIVE_NAME}"
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/v${VERSION}/checksums.txt"

# --- Download archive and checksums ----------------------------------------

echo "Downloading gofasta v${VERSION} (${OS}/${ARCH})..."
curl -fsSL "$ARCHIVE_URL"   -o "${TMP_DIR}/${ARCHIVE_NAME}"
curl -fsSL "$CHECKSUMS_URL" -o "${TMP_DIR}/checksums.txt"

# --- Verify checksum -------------------------------------------------------

# Pick a sha256 implementation. macOS ships shasum; most Linux ships
# sha256sum; some minimal containers ship neither.
if command -v sha256sum >/dev/null 2>&1; then
    SHA256_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
    SHA256_CMD="shasum -a 256"
else
    echo "Neither sha256sum nor shasum is available — cannot verify the download."
    echo "Refusing to install an unverified binary. Install one of:"
    echo "  Linux:  apt-get install coreutils  (or equivalent)"
    echo "  macOS:  shasum ships with the OS — check your PATH."
    exit 1
fi

EXPECTED=$(grep " ${ARCHIVE_NAME}\$" "${TMP_DIR}/checksums.txt" | awk '{print $1}')
if [ -z "$EXPECTED" ]; then
    echo "checksums.txt does not contain an entry for ${ARCHIVE_NAME}."
    echo "The release may be incomplete or the archive name format may have"
    echo "changed. Refusing to install."
    exit 1
fi

ACTUAL=$(cd "$TMP_DIR" && $SHA256_CMD "$ARCHIVE_NAME" | awk '{print $1}')
if [ "$EXPECTED" != "$ACTUAL" ]; then
    echo "Checksum mismatch for ${ARCHIVE_NAME}!"
    echo "  expected: $EXPECTED"
    echo "  actual:   $ACTUAL"
    echo "Refusing to install. Re-run the installer; if the mismatch persists,"
    echo "report it at https://github.com/${REPO}/issues."
    exit 1
fi
echo "Checksum verified."

# --- Extract and install ---------------------------------------------------

tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "$TMP_DIR"
if [ ! -x "${TMP_DIR}/gofasta" ]; then
    echo "Extracted archive does not contain an executable 'gofasta' binary."
    echo "Refusing to install. Report at https://github.com/${REPO}/issues."
    exit 1
fi

if [ -w "$INSTALL_DIR" ]; then
    mv "${TMP_DIR}/gofasta" "$INSTALL_DIR/gofasta"
else
    sudo mv "${TMP_DIR}/gofasta" "$INSTALL_DIR/gofasta"
fi

echo "gofasta v${VERSION} installed to ${INSTALL_DIR}/gofasta"
echo ""

# --- PATH sanity check -----------------------------------------------------

case ":${PATH}:" in
    *":${INSTALL_DIR}:"*)
        # On PATH. Verify the shell actually resolves `gofasta` to our copy.
        RESOLVED=$(command -v gofasta 2>/dev/null || true)
        if [ -z "$RESOLVED" ]; then
            echo "⚠  ${INSTALL_DIR} is on \$PATH but your shell hasn't picked up"
            echo "   the new binary yet. Run 'hash -r' (bash/zsh) or open a new"
            echo "   terminal, then run 'gofasta --version'."
            echo ""
        elif [ "$RESOLVED" != "${INSTALL_DIR}/gofasta" ]; then
            echo "⚠  Your shell resolves 'gofasta' to $RESOLVED, not the copy"
            echo "   we just installed at ${INSTALL_DIR}/gofasta. Another"
            echo "   installation is earlier on \$PATH. Run 'which -a gofasta'"
            echo "   to see all of them."
            echo ""
        fi
        ;;
    *)
        echo "⚠  ${INSTALL_DIR} is not on your \$PATH. Add it to your shell config:"
        echo ""
        SHELL_NAME=$(basename "${SHELL:-}")
        case "$SHELL_NAME" in
            zsh)
                echo "   echo 'export PATH=\"\$PATH:${INSTALL_DIR}\"' >> ~/.zshrc"
                echo "   source ~/.zshrc" ;;
            bash)
                echo "   echo 'export PATH=\"\$PATH:${INSTALL_DIR}\"' >> ~/.bashrc"
                echo "   source ~/.bashrc" ;;
            fish)
                echo "   fish_add_path ${INSTALL_DIR}" ;;
            *)
                echo "   export PATH=\"\$PATH:${INSTALL_DIR}\""
                echo "   (add the line above to your shell's startup file)" ;;
        esac
        echo ""
        ;;
esac

echo "Get started:"
echo "  gofasta new myapp"
echo "  cd myapp"
echo "  gofasta dev"
