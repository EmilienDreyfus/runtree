#!/bin/sh

set -eu

OWNER_REPO="EmilienDreyfus/runtree"
DEFAULT_RELEASES_URL="https://github.com/${OWNER_REPO}/releases"
DEFAULT_API_URL="https://api.github.com/repos/${OWNER_REPO}/releases"

RELEASES_URL="${RUNTREE_BASE_URL:-$DEFAULT_RELEASES_URL}"
API_URL="${RUNTREE_API_BASE_URL:-$DEFAULT_API_URL}"
INSTALL_DIR="${RUNTREE_INSTALL_DIR:-${HOME:-$PWD}/.local/bin}"
REQUESTED_VERSION="${RUNTREE_VERSION:-}"

usage() {
	cat <<'EOF'
Install runtree from GitHub Releases.

Usage:
  sh install.sh [--version <tag>] [--install-dir <path>]

Environment:
  RUNTREE_VERSION      Release tag to install, for example v0.1.0
  RUNTREE_INSTALL_DIR  Target directory for the runtree binary
  RUNTREE_BASE_URL     Override release asset base URL
  RUNTREE_API_BASE_URL Override release API base URL
EOF
}

log() {
	printf '%s\n' "$*" >&2
}

fail() {
	log "error: $*"
	exit 1
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

detect_os() {
	case "$(uname -s)" in
		Darwin) printf 'darwin' ;;
		Linux) printf 'linux' ;;
		*) fail "unsupported operating system: $(uname -s)" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64|amd64) printf 'amd64' ;;
		arm64|aarch64) printf 'arm64' ;;
		*) fail "unsupported architecture: $(uname -m)" ;;
	esac
}

resolve_version() {
	if [ -n "$REQUESTED_VERSION" ]; then
		case "$REQUESTED_VERSION" in
			v*) printf '%s' "$REQUESTED_VERSION" ;;
			*) printf 'v%s' "$REQUESTED_VERSION" ;;
		esac
		return
	fi

	response_file="$1/latest.json"
	curl -fsSL "${API_URL}/latest" -o "$response_file"
	version=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$response_file" | head -n 1)
	[ -n "$version" ] || fail "could not resolve latest release version"
	printf '%s' "$version"
}

download() {
	url="$1"
	output="$2"
	log "downloading ${url}"
	curl -fsSL "$url" -o "$output"
}

hash_file() {
	file="$1"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$file" | awk '{print $1}'
		return
	fi
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$file" | awk '{print $1}'
		return
	fi
	if command -v openssl >/dev/null 2>&1; then
		openssl dgst -sha256 -r "$file" | awk '{print $1}'
		return
	fi
	fail "no SHA-256 tool found (need sha256sum, shasum, or openssl)"
}

verify_checksum() {
	checksums_file="$1"
	archive_file="$2"
	archive_name="$3"

	expected=$(awk -v name="$archive_name" '$2 == name { print $1 }' "$checksums_file")
	[ -n "$expected" ] || fail "checksum for ${archive_name} not found"

	actual=$(hash_file "$archive_file")
	[ "$expected" = "$actual" ] || fail "checksum mismatch for ${archive_name}"
}

main() {
	need_cmd curl
	need_cmd tar
	need_cmd mktemp

	while [ "$#" -gt 0 ]; do
		case "$1" in
			--version)
				[ "$#" -ge 2 ] || fail "--version requires a value"
				REQUESTED_VERSION="$2"
				shift 2
				;;
			--install-dir)
				[ "$#" -ge 2 ] || fail "--install-dir requires a value"
				INSTALL_DIR="$2"
				shift 2
				;;
			-h|--help)
				usage
				exit 0
				;;
			*)
				fail "unknown argument: $1"
				;;
		esac
	done

	tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/runtree-install.XXXXXX")
	trap 'rm -rf "$tmp_dir"' EXIT INT TERM

	os=$(detect_os)
	arch=$(detect_arch)
	version=$(resolve_version "$tmp_dir")
	archive_name="runtree_${version}_${os}_${arch}.tar.gz"
	checksums_name="checksums.txt"
	archive_url="${RELEASES_URL}/download/${version}/${archive_name}"
	checksums_url="${RELEASES_URL}/download/${version}/${checksums_name}"
	archive_path="${tmp_dir}/${archive_name}"
	checksums_path="${tmp_dir}/${checksums_name}"

	download "$checksums_url" "$checksums_path"
	download "$archive_url" "$archive_path"
	verify_checksum "$checksums_path" "$archive_path" "$archive_name"

	tar -xzf "$archive_path" -C "$tmp_dir"
	[ -f "${tmp_dir}/runtree" ] || fail "archive did not contain runtree binary"

	mkdir -p "$INSTALL_DIR"
	install_path="${INSTALL_DIR}/runtree"
	cp "${tmp_dir}/runtree" "$install_path"
	chmod 755 "$install_path"

	log "installed runtree ${version} to ${install_path}"
	"$install_path" version
}

main "$@"
