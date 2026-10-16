#!/bin/sh
# Install a verified release without reading or configuring Trello credentials.
set -eu

fail() {
    printf 'trello-mcp: %s\n' "$*" >&2
    if [ -n "${source_version:-}" ]; then
        printf 'Manual source-build alternative (not executed): go install github.com/avhn/trello-mcp/cmd/trello-mcp@%s\n' "$source_version" >&2
    fi
    exit 1
}

show_help=false
for arg do
    case "$arg" in
        --help|-h) show_help=true ;;
        *) fail "unknown argument: $arg (use --help)" ;;
    esac
done
if [ "$show_help" = true ]; then
    cat <<'EOF'
Usage: sh install.sh [--help|-h]
Install a checksum-verified release into INSTALL_DIR (default: $HOME/.local/bin).
TRELLO_MCP_VERSION selects an exact release tag; omitted means latest release.
Downloads and SHA-256 checksums must succeed; no automatic source build occurs.
Advanced/test download overrides are not supported by this POSIX installer.
No credentials or client configuration are read or changed; PATH is not changed.
Run the printed absolute-path auth command yourself in a private terminal (not through an agent), then use trello Reconnect in /mcp or fully quit/restart the MCP client.
Setup: https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md
EOF
    exit 0
fi

tag=${TRELLO_MCP_VERSION:-}
if [ -n "$tag" ]; then
    case "$tag" in v*) ;; *) tag="v$tag" ;; esac
    case "$tag" in
        *[!a-zA-Z0-9._-]*|v) fail 'TRELLO_MCP_VERSION must be a release tag such as v0.1.0' ;;
    esac
fi
source_version=${tag:-latest}
command -v curl >/dev/null 2>&1 || fail 'curl is required'
install_dir=${INSTALL_DIR:-"$HOME/.local/bin"}
case "$install_dir" in
    /*) ;;
    *) install_dir="$(pwd)/$install_dir" ;;
esac
repo=https://github.com/avhn/trello-mcp
tmp=$(mktemp -d) || fail 'could not create temporary directory'
trap 'rm -rf "$tmp"' 0
trap 'exit 1' HUP INT TERM
# Keep downloads and extraction in separate, new directories.
mkdir "$tmp/archive" "$tmp/checksums" "$tmp/unpack"

os=$(uname -s)
case "$os" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    MINGW*|MSYS*|CYGWIN*) os=windows ;;
    *) os=unsupported ;;
esac
arch=$(uname -m)
case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) arch=unsupported ;;
esac
binary=trello-mcp
extension=tar.gz
if [ "$os" = windows ]; then
    binary=trello-mcp.exe
    extension=zip
fi
asset="trello-mcp_${os}_${arch}.${extension}"

unavailable() {
    fail "$*; no automatic source build was attempted"
}

download_release() {
    if [ -z "$tag" ]; then
        info=$(curl --proto '=https' --proto-redir '=https' -sSL -o /dev/null \
            -w '%{http_code} %{url_effective}' "$repo/releases/latest") \
            || fail 'could not resolve the latest release'
        case "$info" in
            '200 '*) url=${info#* }; tag=${url##*/} ;;
            '404 '*) unavailable 'no published release is available' ;;
            *) fail "could not resolve the latest release (HTTP ${info%% *})" ;;
        esac
    fi
    case "$tag" in
        v*) ;;
        *) tag="v$tag" ;;
    esac
    case "$tag" in
        *[!a-zA-Z0-9._-]*|v) fail 'TRELLO_MCP_VERSION must be a release tag such as v0.1.0' ;;
    esac
    source_version=$tag
    url="$repo/releases/download/$tag"
    status=$(curl --proto '=https' --proto-redir '=https' -sSL \
        -o "$tmp/archive/$asset" -w '%{http_code}' "$url/$asset") \
        || fail "could not download $asset"
    case "$status" in
        200) ;;
        404) unavailable "no release asset $asset exists for $tag" ;;
        *) fail "could not download $asset (HTTP $status)" ;;
    esac
    curl --proto '=https' --proto-redir '=https' -fsSL \
        -o "$tmp/checksums/checksums.txt" "$url/checksums.txt" \
        || fail 'could not download checksums.txt'
    expected=$(awk -v name="$asset" '
        $2 == name || $2 == "*" name { print $1; count++ }
        END { if (count != 1) exit 1 }
    ' "$tmp/checksums/checksums.txt") || fail "missing or duplicate checksum for $asset"
    case "$expected" in
        *[!0-9a-f]*) fail "invalid checksum for $asset" ;;
    esac
    [ "${#expected}" -eq 64 ] || fail "invalid checksum for $asset"
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "$tmp/archive/$asset") || fail 'sha256sum failed'
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "$tmp/archive/$asset") || fail 'shasum failed'
    else
        fail 'sha256sum or shasum is required to verify the download'
    fi
    [ "${actual%% *}" = "$expected" ] || fail "checksum verification failed for $asset"
    if [ "$extension" = zip ]; then
        command -v unzip >/dev/null 2>&1 || fail 'unzip is required for Windows archives'
        unzip -q "$tmp/archive/$asset" "$binary" -d "$tmp/unpack" \
            || fail 'could not extract the release archive'
    else
        tar -xzf "$tmp/archive/$asset" -C "$tmp/unpack" "$binary" \
            || fail 'could not extract the release archive'
    fi
    [ -f "$tmp/unpack/$binary" ] && [ ! -L "$tmp/unpack/$binary" ] \
        || fail 'archive does not contain a regular binary'
    mkdir -p "$install_dir" || fail "cannot create $install_dir"
    cp "$tmp/unpack/$binary" "$install_dir/$binary" || fail "cannot install to $install_dir"
    chmod 755 "$install_dir/$binary" || fail 'could not make the binary executable'
}

case "$os/$arch" in
    linux/amd64|linux/arm64|darwin/amd64|darwin/arm64|windows/amd64) download_release ;;
    *) unavailable 'no release asset is available for this platform' ;;
esac
printf 'Installed %s\n' "$install_dir/$binary"
case ":${PATH:-}:" in
    *:"$install_dir":*) ;;
    *) printf 'Warning: %s is not on PATH. Add it to your shell and MCP client PATH.\n' "$install_dir" >&2 ;;
esac

client_binary="$install_dir/$binary"
# Windows desktop clients need a native path, not Git Bash/MSYS/Cygwin notation.
if [ "$os" = windows ] && command -v cygpath >/dev/null 2>&1; then
    client_binary=$(cygpath -w "$client_binary") || fail 'could not convert the Windows executable path'
fi
# Reuse one single-argument quote for every human command and registration.
quote_argument() {
    printf "'"
    printf '%s' "$1" | sed "s/'/'\\\\''/g"
    printf "'"
}
quoted_binary=$(quote_argument "$client_binary")
printf '\nRun the printed absolute-path auth command yourself in a private terminal (not through an agent), then use trello Reconnect in /mcp or fully quit/restart the MCP client.\n'
printf 'Installed executable: %s\n' "$client_binary"
printf 'Auth: %s auth\n' "$quoted_binary"
printf 'Read-only auth: %s auth --read-only\n' "$quoted_binary"
printf 'Status: %s auth status\n' "$quoted_binary"
printf 'Version: %s --version\n' "$quoted_binary"
printf 'Credentials: ~/.secrets/.trello-mcp.secret; override with absolute TRELLO_MCP_CREDENTIALS or auth --file PATH.\n'
printf 'Read-only opt-in: TRELLO_READ_ONLY=1 or the read-only auth command above.\n'
printf '\nRegister with Claude Code (user scope):\n'
printf 'claude mcp add --scope user --transport stdio trello -- %s\n' "$quoted_binary"
printf '\nSetup, other clients and explicit local/project scope alternatives:\n'
printf 'https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md\n'
