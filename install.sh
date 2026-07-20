#!/usr/bin/env sh
set -eu

REPO="${JIRACTRL_REPO:-owainlewis/jiractrl}"
VERSION="${JIRACTRL_VERSION:-latest}"
INSTALL_DIR="${JIRACTRL_INSTALL_DIR:-${HOME}/.local/bin}"

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
checksum_asset="${asset}.sha256"

if [ "$VERSION" = "latest" ]; then
  url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
  url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
fi
checksum_url="${url}.sha256"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $url"
curl -fsSL "$url" -o "$tmp/$asset"
curl -fsSL "$checksum_url" -o "$tmp/$checksum_asset"

expected_checksum="$(awk 'NR == 1 { print $1 }' "$tmp/$checksum_asset")"
case "$expected_checksum" in
  *[!0-9a-fA-F]*|'')
    echo "invalid checksum file: $checksum_url" >&2
    exit 1
    ;;
esac

if [ "${#expected_checksum}" -ne 64 ]; then
  echo "invalid checksum file: $checksum_url" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "$tmp/$asset" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  actual_checksum="$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')"
else
  echo "sha256sum or shasum is required to verify the download" >&2
  exit 1
fi

if [ "$actual_checksum" != "$expected_checksum" ]; then
  echo "checksum verification failed for $asset" >&2
  exit 1
fi

echo "Checksum verified"
tar -xzf "$tmp/$asset" -C "$tmp"

mkdir -p "$INSTALL_DIR"
cp "$tmp/jiractrl" "$INSTALL_DIR/jiractrl"
chmod +x "$INSTALL_DIR/jiractrl"

echo "Installed jiractrl to $INSTALL_DIR/jiractrl"

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "Add $INSTALL_DIR to PATH to run jiractrl from any directory." ;;
esac
