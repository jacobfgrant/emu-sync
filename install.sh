#!/bin/sh
set -e

# emu-sync installer
# Usage: curl -sSL https://raw.githubusercontent.com/jacobfgrant/emu-sync/master/install.sh | bash

REPO="jacobfgrant/emu-sync"
INSTALL_DIR="$HOME/.local/bin"

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)
        echo "Error: unsupported architecture: $ARCH" >&2
        exit 1
        ;;
esac

case "$OS" in
    linux|darwin) ;;
    *)
        echo "Error: unsupported OS: $OS" >&2
        exit 1
        ;;
esac

# Linux arm64 is not a supported target
if [ "$OS" = "linux" ] && [ "$ARCH" = "arm64" ]; then
    echo "Error: Linux arm64 is not supported" >&2
    exit 1
fi

echo "Detected: ${OS}/${ARCH}"

# Get latest release tag
LATEST=$(curl -sSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4)

if [ -z "$LATEST" ]; then
    echo "Error: could not determine latest release" >&2
    exit 1
fi

echo "Latest release: ${LATEST}"

# Download and extract
FILENAME="emu-sync_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${LATEST}/${FILENAME}"

echo "Downloading ${URL}..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -sSL "$URL" -o "${TMPDIR}/${FILENAME}"

mkdir -p "$INSTALL_DIR"
tar -xzf "${TMPDIR}/${FILENAME}" -C "$TMPDIR"
mv "${TMPDIR}/emu-sync" "${INSTALL_DIR}/emu-sync"
chmod +x "${INSTALL_DIR}/emu-sync"

echo "Installed emu-sync to ${INSTALL_DIR}/emu-sync"

# Check if install dir is in PATH
case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
        echo ""
        echo "NOTE: ${INSTALL_DIR} is not in your PATH."
        echo "Add this to your shell profile:"
        echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
        ;;
esac

echo ""
echo "Run 'emu-sync --help' to get started."
