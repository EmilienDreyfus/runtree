#!/bin/sh

set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
VERSION="${VERSION:-v0.1.0}"
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/runtree-smoke.XXXXXX")
DIST_DIR="${TMP_DIR}/dist"
SERVER_ROOT="${TMP_DIR}/server"
INSTALL_DIR="${TMP_DIR}/bin"
RUNTREE_HOME_DIR="${TMP_DIR}/home"
FAKE_TUNNEL="${TMP_DIR}/cloudflared"

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		printf '%s\n' "required command not found: $1" >&2
		exit 1
	}
}

cleanup() {
	rm -rf "$TMP_DIR"
}

trap cleanup EXIT INT TERM

need_cmd curl

cat > "$FAKE_TUNNEL" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 755 "$FAKE_TUNNEL"

mkdir -p "$SERVER_ROOT/releases/download/${VERSION}" "$SERVER_ROOT/api/releases"

case "$(uname -s)" in
	Darwin) tunnel_platform_os="DARWIN" ;;
	Linux) tunnel_platform_os="LINUX" ;;
	*) printf '%s\n' "unsupported smoke test platform: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
	x86_64|amd64) tunnel_platform_arch="AMD64" ;;
	arm64|aarch64) tunnel_platform_arch="ARM64" ;;
	*) printf '%s\n' "unsupported smoke test architecture: $(uname -m)" >&2; exit 1 ;;
esac

tunnel_env_var="TUNNEL_BINARY_${tunnel_platform_os}_${tunnel_platform_arch}"

(
	cd "$ROOT_DIR"
	eval "$tunnel_env_var=\"\$FAKE_TUNNEL\" VERSION=\"\$VERSION\" OUT_DIR=\"\$DIST_DIR\" ./scripts/build-release.sh"
)

cp "$DIST_DIR"/checksums.txt "$SERVER_ROOT/releases/download/${VERSION}/"
cp "$DIST_DIR"/runtree_"${VERSION}"_*.tar.gz "$SERVER_ROOT/releases/download/${VERSION}/"
printf '{"tag_name":"%s"}\n' "$VERSION" > "$SERVER_ROOT/api/releases/latest"

RELEASES_URL="file://${SERVER_ROOT}/releases"
API_URL="file://${SERVER_ROOT}/api/releases"

RUNTREE_BASE_URL="$RELEASES_URL" \
RUNTREE_API_BASE_URL="$API_URL" \
RUNTREE_INSTALL_DIR="$INSTALL_DIR" \
RUNTREE_HOME="$RUNTREE_HOME_DIR" \
	sh "$ROOT_DIR/install.sh"

"$INSTALL_DIR/runtree" version | grep -q "$VERSION"
test -x "$RUNTREE_HOME_DIR/bin/cloudflared"
rm -f "$INSTALL_DIR/runtree"

RUNTREE_BASE_URL="$RELEASES_URL" \
RUNTREE_API_BASE_URL="$API_URL" \
RUNTREE_INSTALL_DIR="$INSTALL_DIR" \
RUNTREE_VERSION="$VERSION" \
RUNTREE_HOME="$RUNTREE_HOME_DIR" \
	sh "$ROOT_DIR/install.sh"

"$INSTALL_DIR/runtree" --version | grep -q "$VERSION"
