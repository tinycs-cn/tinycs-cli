#!/bin/sh
# tinycs-cli installer for Linux and macOS
# Usage: curl -fsSL https://github.com/tinycs-cn/tinycs-cli/releases/latest/download/install.sh | sh
set -e

REPO="tinycs-cn/tinycs-cli"
INSTALL_DIR="/usr/local/bin"
BIN="tinycs"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64)  arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *)
    echo "Unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

case "$os" in
  linux|darwin) ;;
  *)
    echo "Unsupported OS: $os. For Windows, see: https://github.com/$REPO#windows" >&2
    exit 1
    ;;
esac

URL="https://github.com/${REPO}/releases/latest/download/${BIN}-${os}-${arch}"
DEST="${INSTALL_DIR}/${BIN}"

echo "Downloading tinycs ($os/$arch)..."
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$URL" -o "$DEST"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$DEST" "$URL"
else
  echo "Error: curl or wget is required" >&2
  exit 1
fi

chmod +x "$DEST"
echo "Installed: $DEST"
"$DEST" version
