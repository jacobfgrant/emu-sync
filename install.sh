#!/bin/sh
set -e

# emu-sync installer
# Usage:
#   curl -sSL https://raw.githubusercontent.com/jacobfgrant/emu-sync/master/install.sh | bash
#   curl -sSL https://raw.githubusercontent.com/jacobfgrant/emu-sync/master/install.sh | bash -s -- <token>

REPO="jacobfgrant/emu-sync"
INSTALL_DIR="$HOME/.local/bin"
TOKEN="$1"

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

# Download archive and checksums
FILENAME="emu-sync_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${LATEST}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading ${FILENAME}..."
curl -sSL "${BASE_URL}/${FILENAME}" -o "${TMPDIR}/${FILENAME}"
curl -sSL "${BASE_URL}/checksums.txt" -o "${TMPDIR}/checksums.txt"

# Verify checksum
echo "Verifying checksum..."
EXPECTED=$(grep "${FILENAME}" "${TMPDIR}/checksums.txt" | cut -d' ' -f1)
if [ -z "$EXPECTED" ]; then
    echo "Error: checksum not found for ${FILENAME}" >&2
    exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL=$(sha256sum "${TMPDIR}/${FILENAME}" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
    ACTUAL=$(shasum -a 256 "${TMPDIR}/${FILENAME}" | cut -d' ' -f1)
else
    echo "Warning: no sha256sum or shasum found, skipping verification" >&2
    ACTUAL="$EXPECTED"
fi

if [ "$EXPECTED" != "$ACTUAL" ]; then
    echo "Error: checksum mismatch" >&2
    echo "  Expected: ${EXPECTED}" >&2
    echo "  Got:      ${ACTUAL}" >&2
    exit 1
fi
echo "Checksum OK"

# Extract and install
mkdir -p "$INSTALL_DIR"
tar -xzf "${TMPDIR}/${FILENAME}" -C "$TMPDIR"
mv "${TMPDIR}/emu-sync" "${INSTALL_DIR}/emu-sync"
chmod +x "${INSTALL_DIR}/emu-sync"

echo "Installed emu-sync to ${INSTALL_DIR}/emu-sync"

# Add to PATH for this session if needed
case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
        export PATH="${INSTALL_DIR}:${PATH}"
        echo "Added ${INSTALL_DIR} to PATH for this session"
        echo ""
        echo "To make this permanent, add to your shell profile:"
        echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
        ;;
esac

# If a token was provided, run setup and install
if [ -n "$TOKEN" ]; then
    echo ""
    echo "Configuring with setup token..."
    emu-sync setup "$TOKEN"

    if [ "$OS" = "linux" ]; then
        echo ""
        echo "Installing systemd timer..."
        emu-sync install
    fi
fi

echo ""
echo "Done! Run 'emu-sync --help' to get started."
