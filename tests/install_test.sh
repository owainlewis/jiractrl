#!/usr/bin/env sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf "$test_root"' EXIT

release_dir="$test_root/release"
mock_bin="$test_root/bin"
install_dir="$test_root/install"
mkdir -p "$release_dir" "$mock_bin" "$install_dir"

printf '#!/usr/bin/env sh\necho jiractrl-test\n' > "$test_root/jiractrl"
chmod +x "$test_root/jiractrl"
tar -C "$test_root" -czf "$release_dir/jiractrl_linux_amd64.tar.gz" jiractrl

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$release_dir/jiractrl_linux_amd64.tar.gz" > "$release_dir/jiractrl_linux_amd64.tar.gz.sha256"
else
  shasum -a 256 "$release_dir/jiractrl_linux_amd64.tar.gz" > "$release_dir/jiractrl_linux_amd64.tar.gz.sha256"
fi

cat > "$mock_bin/uname" <<'EOF'
#!/usr/bin/env sh
case "$1" in
  -s) printf 'Linux\n' ;;
  -m) printf 'x86_64\n' ;;
  *) exit 1 ;;
esac
EOF

cat > "$mock_bin/curl" <<'EOF'
#!/usr/bin/env sh
url=''
output=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
cp "$MOCK_RELEASE_DIR/${url##*/}" "$output"
EOF

chmod +x "$mock_bin/uname" "$mock_bin/curl"

PATH="$mock_bin:$PATH" \
  MOCK_RELEASE_DIR="$release_dir" \
  JIRACTRL_INSTALL_DIR="$install_dir" \
  sh "$repo_root/install.sh"

test "$("$install_dir/jiractrl")" = "jiractrl-test"
PATH="$mock_bin:/usr/bin:/bin" \
  MOCK_RELEASE_DIR="$release_dir" \
  JIRACTRL_INSTALL_DIR="$install_dir" \
  sh "$repo_root/install.sh" > "$test_root/path-notice.out"
grep -q "Add $install_dir to PATH" "$test_root/path-notice.out"

printf 'tampered archive\n' > "$release_dir/jiractrl_linux_amd64.tar.gz"
rm -f "$install_dir/jiractrl"

if PATH="$mock_bin:$PATH" \
  MOCK_RELEASE_DIR="$release_dir" \
  JIRACTRL_INSTALL_DIR="$install_dir" \
  sh "$repo_root/install.sh" > "$test_root/tampered.out" 2>&1; then
  echo "installer accepted an archive with the wrong checksum" >&2
  exit 1
fi

grep -q "checksum verification failed" "$test_root/tampered.out"
test ! -e "$install_dir/jiractrl"

echo "installer tests passed"
