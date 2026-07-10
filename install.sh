#!/usr/bin/env sh
set -eu

REPO="${JIRACTRL_REPO:-owainlewis/jirac}"
VERSION="${JIRACTRL_VERSION:-latest}"
INSTALL_DIR="${JIRACTRL_INSTALL_DIR:-/usr/local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

case "$os" in
  darwin|linux) ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

asset="jiractrl_${os}_${arch}.tar.gz"

if [ "$VERSION" = "latest" ]; then
  url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
  url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $url"
curl -fsSL "$url" -o "$tmp/$asset"
tar -xzf "$tmp/$asset" -C "$tmp"

mkdir -p "$INSTALL_DIR"
cp "$tmp/jiractrl" "$INSTALL_DIR/jiractrl"
chmod +x "$INSTALL_DIR/jiractrl"

echo "Installed jiractrl to $INSTALL_DIR/jiractrl"
