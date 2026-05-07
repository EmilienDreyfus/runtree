#!/bin/sh

set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
OUT_DIR="${OUT_DIR:-${ROOT_DIR}/dist}"
VERSION="${VERSION:-${GITHUB_REF_NAME:-}}"
GOCACHE="${GOCACHE:-${ROOT_DIR}/.cache/go-build}"
GOMODCACHE="${GOMODCACHE:-${ROOT_DIR}/.cache/gomod}"

[ -n "$VERSION" ] || {
	printf '%s\n' "VERSION is required" >&2
	exit 1
}

COMMIT="${COMMIT:-$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || printf 'unknown')}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LD_FLAGS="-s -w -X github.com/EmilienDreyfus/runtree/internal/buildinfo.Version=${VERSION} -X github.com/EmilienDreyfus/runtree/internal/buildinfo.Commit=${COMMIT} -X github.com/EmilienDreyfus/runtree/internal/buildinfo.Date=${DATE}"

mkdir -p "$OUT_DIR" "$GOCACHE" "$GOMODCACHE"
rm -f "$OUT_DIR"/runtree_*.tar.gz "$OUT_DIR"/checksums.txt

build_target() {
	goos="$1"
	goarch="$2"
	staging_dir=$(mktemp -d "${TMPDIR:-/tmp}/runtree-release.XXXXXX")
	archive_name="runtree_${VERSION}_${goos}_${goarch}.tar.gz"
	tunnel_var="TUNNEL_BINARY_$(printf '%s_%s' "$goos" "$goarch" | tr '[:lower:]/-' '[:upper:]___')"

	GOCACHE="$GOCACHE" GOMODCACHE="$GOMODCACHE" CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
		go build -trimpath -ldflags "$LD_FLAGS" -o "${staging_dir}/runtree" ./cmd/runtree

	cp "$ROOT_DIR/README.md" "$ROOT_DIR/LICENSE" "$staging_dir/"
	eval "tunnel_binary=\${$tunnel_var:-}"
	if [ -n "$tunnel_binary" ]; then
		cp "$tunnel_binary" "${staging_dir}/cloudflared"
		chmod 755 "${staging_dir}/cloudflared"
		tar -C "$staging_dir" -czf "${OUT_DIR}/${archive_name}" runtree cloudflared README.md LICENSE
	else
		tar -C "$staging_dir" -czf "${OUT_DIR}/${archive_name}" runtree README.md LICENSE
	fi
	rm -rf "$staging_dir"
}

build_target darwin amd64
build_target darwin arm64
build_target linux amd64
build_target linux arm64

if command -v sha256sum >/dev/null 2>&1; then
	(
		cd "$OUT_DIR"
		sha256sum runtree_*.tar.gz > checksums.txt
	)
elif command -v shasum >/dev/null 2>&1; then
	(
		cd "$OUT_DIR"
		shasum -a 256 runtree_*.tar.gz > checksums.txt
	)
else
	printf '%s\n' "need sha256sum or shasum to generate checksums" >&2
	exit 1
fi
