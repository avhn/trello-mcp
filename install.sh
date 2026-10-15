#!/bin/sh
# Install a verified release without reading or configuring Trello credentials.
set -eu

fail() {
    printf 'trello-mcp: %s\n' "$*" >&2
    exit 1
}

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

fallback() {
    command -v go >/dev/null 2>&1 || fail "$*; install Go to build from source"
    printf 'trello-mcp: %s; falling back to go install at @latest\n' "$*" >&2
    mkdir -p "$install_dir" || fail "cannot create $install_dir"
    GOBIN="$install_dir" go install github.com/avhn/trello-mcp/cmd/trello-mcp@latest \
        || fail 'go install failed'
}

download_release() {
    tag=${TRELLO_MCP_VERSION:-}
    if [ -z "$tag" ]; then
        info=$(curl --proto '=https' --proto-redir '=https' -sSL -o /dev/null \
            -w '%{http_code} %{url_effective}' "$repo/releases/latest") \
            || fail 'could not resolve the latest release'
        case "$info" in
            '200 '*) url=${info#* }; tag=${url##*/} ;;
            '404 '*) fallback 'no published release is available'; return ;;
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
    url="$repo/releases/download/$tag"
    status=$(curl --proto '=https' --proto-redir '=https' -sSL \
        -o "$tmp/archive/$asset" -w '%{http_code}' "$url/$asset") \
        || fail "could not download $asset"
    case "$status" in
        200) ;;
        404) fallback "no release asset $asset exists for $tag"; return ;;
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
    *) fallback 'no release asset is available for this platform' ;;
esac
printf 'Installed %s\n' "$install_dir/$binary"
case ":${PATH:-}:" in
    *:"$install_dir":*) ;;
    *) printf 'Warning: %s is not on PATH. Add it to your shell and MCP client PATH.\n' "$install_dir" >&2 ;;
esac

if command -v claude >/dev/null 2>&1; then
    printf '\nRegister with Claude Code (replace placeholders locally, then run):\n'
    printf 'claude mcp add trello --transport stdio --env TRELLO_API_KEY=YOUR_API_KEY --env TRELLO_TOKEN=YOUR_TOKEN --env TRELLO_READ_ONLY=1 -- '
    # Quote the installed path as a single shell argument, including embedded quotes.
    printf "'"
    printf '%s' "$install_dir/$binary" | sed "s/'/'\\\\''/g"
    printf "'\n"
fi
cat <<'EOF'

For other MCP clients, merge this into the client configuration.
Replace placeholders locally. Use the full installed path if trello-mcp is not on the client's PATH.
{
  "mcpServers": {
    "trello": {
      "command": "trello-mcp",
      "env": {
        "TRELLO_API_KEY": "YOUR_API_KEY",
        "TRELLO_TOKEN": "YOUR_TOKEN",
        "TRELLO_READ_ONLY": "1"
      }
    }
  }
}
EOF
