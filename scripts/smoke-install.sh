#!/bin/sh

set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
VERSION="${VERSION:-v0.1.0}"
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/runtree-smoke.XXXXXX")
DIST_DIR="${TMP_DIR}/dist"
SERVER_ROOT="${TMP_DIR}/server"
INSTALL_DIR="${TMP_DIR}/bin"

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

mkdir -p "$SERVER_ROOT/releases/download/${VERSION}" "$SERVER_ROOT/api/releases"

(
	cd "$ROOT_DIR"
	VERSION="$VERSION" OUT_DIR="$DIST_DIR" ./scripts/build-release.sh
)

cp "$DIST_DIR"/checksums.txt "$SERVER_ROOT/releases/download/${VERSION}/"
cp "$DIST_DIR"/runtree_"${VERSION}"_*.tar.gz "$SERVER_ROOT/releases/download/${VERSION}/"
printf '{"tag_name":"%s"}\n' "$VERSION" > "$SERVER_ROOT/api/releases/latest"

RELEASES_URL="file://${SERVER_ROOT}/releases"
API_URL="file://${SERVER_ROOT}/api/releases"

RUNTREE_BASE_URL="$RELEASES_URL" \
RUNTREE_API_BASE_URL="$API_URL" \
RUNTREE_INSTALL_DIR="$INSTALL_DIR" \
	sh "$ROOT_DIR/install.sh"

"$INSTALL_DIR/runtree" version | grep -q "$VERSION"
rm -f "$INSTALL_DIR/runtree"

RUNTREE_BASE_URL="$RELEASES_URL" \
RUNTREE_API_BASE_URL="$API_URL" \
RUNTREE_INSTALL_DIR="$INSTALL_DIR" \
RUNTREE_VERSION="$VERSION" \
	sh "$ROOT_DIR/install.sh"

"$INSTALL_DIR/runtree" --version | grep -q "$VERSION"
