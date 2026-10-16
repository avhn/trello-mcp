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

# Style only interactive terminals; NO_COLOR and TERM=dumb keep plain text.
style() {
    if [ -t "$1" ] && [ -z "${NO_COLOR:-}" ] && [ "${TERM:-dumb}" != dumb ]; then
        printf '\033[%sm' "$2"
    fi
}
bold=$(style 1 1) dim=$(style 1 2) cmd=$(style 1 36) reset=$(style 1 0)
warn=$(style 2 33) warn_reset=$(style 2 0) warn_cmd=$(style 2 36)

printf '\n%strello-mcp %s installed to %s%s\n' "$bold" "$tag" "$client_binary" "$reset"
case ":${PATH:-}:" in
    *:"$install_dir":*) ;;
    *)
        # Suggest a profile line only for paths every supported shell reads unquoted.
        path_line='' profile=''
        case "$install_dir" in
            *[!a-zA-Z0-9._/-]*) ;;
            *)
                display_dir=$install_dir
                case "$install_dir" in "$HOME"/*) display_dir="\$HOME/${install_dir#"$HOME"/}" ;; esac
                case "${SHELL:-}" in
                    */bash) profile=.bashrc path_line="export PATH=\"$display_dir:\$PATH\"" ;;
                    */zsh) profile=.zshrc path_line="export PATH=\"$display_dir:\$PATH\"" ;;
                    */fish) path_line="fish_add_path $display_dir" ;;
                esac ;;
        esac
        {
            printf '\n  %sWarning: %s is not on your PATH.%s\n' "$warn" "$install_dir" "$warn_reset"
            if [ -n "$path_line" ] && [ -n "$profile" ]; then
                printf '  To run trello-mcp by name, add this to ~/%s and open a new terminal:\n\n' "$profile"
                printf '    %s%s%s\n\n' "$warn_cmd" "$path_line" "$warn_reset"
            elif [ -n "$path_line" ]; then
                printf '  To run trello-mcp by name, run this once and open a new terminal:\n\n'
                printf '    %s%s%s\n\n' "$warn_cmd" "$path_line" "$warn_reset"
            else
                printf '  To run trello-mcp by name, add that directory to PATH in your shell profile.\n'
            fi
            printf '  The commands below use the full path, so they work either way.\n'
        } >&2 ;;
esac

printf '\n%sNext steps%s\n\n' "$bold" "$reset"
printf '  1. Save your Trello API key and token. Run this yourself in a private\n'
printf '     terminal, not through an agent:\n\n'
printf '       %s%s auth%s\n\n' "$cmd" "$quoted_binary" "$reset"
printf '     %sFor a read-only token, add the --read-only flag.%s\n\n' "$dim" "$reset"
printf '  2. Register with Claude Code (user scope):\n\n'
printf '       %sclaude mcp add --scope user --transport stdio trello -- %s%s\n\n' "$cmd" "$quoted_binary" "$reset"
printf '     %sOther clients: https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md%s\n\n' "$dim" "$reset"
printf '  3. Reconnect: use trello Reconnect in /mcp, or fully quit and restart your MCP client.\n'
printf '\n%sOther commands%s\n' "$bold" "$reset"
printf '  Status    %s%s auth status%s\n' "$cmd" "$quoted_binary" "$reset"
printf '  Logout    %s%s auth logout%s\n' "$cmd" "$quoted_binary" "$reset"
printf '  Version   %s%s --version%s\n' "$cmd" "$quoted_binary" "$reset"
printf '\n%sCredentials: ~/.secrets/.trello-mcp.secret (override with absolute TRELLO_MCP_CREDENTIALS\n' "$dim"
printf 'or auth --file PATH). TRELLO_READ_ONLY=1 hides write and transfer tools.%s\n' "$reset"
