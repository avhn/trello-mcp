#!/bin/sh
# Offline installer integration tests. All downloads and Go installs are synthetic.
set -eu
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' 0
trap 'exit 1' HUP INT TERM
mkdir "$tmp/bin" "$tmp/source" "$tmp/archives" "$tmp/checksums" "$tmp/runs"
printf 'synthetic release binary\n' > "$tmp/source/trello-mcp"
for platform in linux_amd64 darwin_arm64; do
    tar -czf "$tmp/archives/trello-mcp_$platform.tar.gz" -C "$tmp/source" trello-mcp
done
windows=false
if command -v zip >/dev/null 2>&1 && command -v unzip >/dev/null 2>&1; then
    windows=true
    cp "$tmp/source/trello-mcp" "$tmp/source/trello-mcp.exe"
    zip -jq "$tmp/archives/trello-mcp_windows_amd64.zip" "$tmp/source/trello-mcp.exe"
fi
if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp/archives" && sha256sum ./*) > "$tmp/checksums/checksums.txt"
else
    (cd "$tmp/archives" && shasum -a 256 ./*) > "$tmp/checksums/checksums.txt"
fi
# Match the release manifest's bare archive names.
sed 's|  \./|  |' "$tmp/checksums/checksums.txt" > "$tmp/checksums/manifest"
cat > "$tmp/bin/curl" <<'EOF'
#!/bin/sh
set -eu
output= format=
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) output=$2; shift 2 ;;
        -w) format=$2; shift 2 ;;
        --proto|--proto-redir) shift 2 ;;
        -*) shift ;;
        *) url=$1; shift ;;
    esac
done
printf '%s\n' "$url" >> "$TEST_LOG"
case "$url" in
    */releases/latest)
        if [ "$TEST_CASE" = no-release ]; then
            printf '404 https://github.com/avhn/trello-mcp/releases/latest'
        elif [ "$TEST_CASE" = network ]; then
            exit 7
        else
            printf '200 https://github.com/avhn/trello-mcp/releases/tag/v1.2.3'
        fi ;;
    */checksums.txt)
        case "$TEST_CASE" in
            missing-checksums) exit 22 ;;
            bad-manifest) printf 'not-a-hash  trello-mcp_linux_amd64.tar.gz\n' > "$output" ;;
            duplicate) cat "$TEST_FIXTURES/checksums/manifest" "$TEST_FIXTURES/checksums/manifest" > "$output" ;;
            *) cp "$TEST_FIXTURES/checksums/manifest" "$output" ;;
        esac ;;
    *)
        case "$TEST_CASE" in
            missing) printf 'not found' > "$output"; printf '404' ;;
            forbidden) printf 'forbidden' > "$output"; printf '403' ;;
            corrupt) printf 'corrupt archive' > "$output"; printf '200' ;;
            *) cp "$TEST_FIXTURES/archives/${url##*/}" "$output"; printf '200' ;;
        esac ;;
esac
EOF
cat > "$tmp/bin/uname" <<'EOF'
#!/bin/sh
case "$1" in
    -s) printf '%s\n' "$TEST_OS" ;;
    -m) printf '%s\n' "$TEST_ARCH" ;;
esac
EOF
cat > "$tmp/bin/go" <<'EOF'
#!/bin/sh
set -eu
printf 'go %s\n' "$*" >> "$TEST_LOG"
[ "$*" = 'install github.com/avhn/trello-mcp/cmd/trello-mcp@latest' ]
printf 'synthetic source binary\n' > "$GOBIN/trello-mcp"
chmod 755 "$GOBIN/trello-mcp"
EOF
cat > "$tmp/bin/claude" <<'EOF'
#!/bin/sh
printf 'ERROR: installer ran claude\n' >> "$TEST_LOG"
exit 1
EOF
cat > "$tmp/bin/cygpath" <<'EOF'
#!/bin/sh
[ "$1" = -w ] || exit 1
printf '%s\n' 'C:\synthetic\trello-mcp.exe'
EOF
chmod +x "$tmp/bin/"*

# Restricted PATHs exercise missing tools without depending on the host's Go setup.
for variant in no-go shasum no-hash; do
    mkdir "$tmp/$variant"
    for tool in sh curl uname mktemp rm mkdir awk sed cp chmod tar gzip cat claude; do
        ln -s "$(PATH="$tmp/bin:$PATH" command -v "$tool")" "$tmp/$variant/$tool"
    done
    if [ "$variant" != no-go ]; then
        ln -s "$tmp/bin/go" "$tmp/$variant/go"
    fi
    if [ "$variant" = no-go ]; then
        if command -v sha256sum >/dev/null 2>&1; then
            ln -s "$(command -v sha256sum)" "$tmp/$variant/sha256sum"
        else
            ln -s "$(command -v shasum)" "$tmp/$variant/shasum"
        fi
    elif [ "$variant" = shasum ] && command -v shasum >/dev/null 2>&1; then
        ln -s "$(command -v shasum)" "$tmp/$variant/shasum"
    fi
done

assert_not_logged() {
    if grep -q "$1" "$2"; then
        printf 'FAIL: unexpected log entry %s in %s\n' "$1" "$2" >&2
        exit 1
    fi
}

run_case() {
    name=$1 mode=$2 os=$3 arch=$4 version=$5 expected=$6
    run="$tmp/runs/$name"
    mkdir "$run" "$run/home" "$run/temp"
    : > "$run/log"
    destination="$run/bin with 'quote"
    if [ "$name" = escaping ]; then
        destination="$run/bin with \"quote\\slash"
    fi
    binary=trello-mcp
    case "$os" in MINGW*|MSYS*|CYGWIN*) binary=trello-mcp.exe ;; esac
    if env -i PATH="${7:-$tmp/bin:$PATH}" HOME="$run/home" TMPDIR="$run/temp" \
        INSTALL_DIR="$destination" TRELLO_MCP_VERSION="$version" \
        TEST_CASE="$mode" TEST_OS="$os" TEST_ARCH="$arch" TEST_LOG="$run/log" \
        TEST_FIXTURES="$tmp" sh "$root/install.sh" > "$run/out" 2> "$run/err"; then
        status=pass
    else
        status=fail
    fi
    if [ "$status" != "$expected" ]; then
        cat "$run/out" "$run/err" >&2
        printf 'FAIL %s: expected %s, got %s\n' "$name" "$expected" "$status" >&2
        exit 1
    fi
    [ -z "$(ls -A "$run/temp")" ] || { printf 'FAIL %s: temporary files remain\n' "$name"; exit 1; }
    if [ "$expected" = pass ]; then
        [ -x "$destination/$binary" ]
        grep -Fq 'Next: run `' "$run/out"
        grep -Fq 'auth` to connect your Trello account' "$run/out"
        grep -Fq 'TRELLO_READ_ONLY=1 or trello-mcp auth --read-only' "$run/out"
        grep -Fq 'set both TRELLO_API_KEY and TRELLO_TOKEN' "$run/out"
        assert_not_logged 'YOUR_API_KEY\|YOUR_TOKEN\|--env\|"env"\|TRELLO_READ_ONLY =\|TRELLO_READ_ONLY"' "$run/out"
        grep -q '"mcpServers"' "$run/out"
        grep -q 'Claude Desktop' "$run/out"
        grep -Fq '[mcp_servers.trello]' "$run/out"
        assert_not_logged '\[mcp_servers.trello.env\]' "$run/out"
        if [ "$name" = escaping ]; then
            encoded="\"$run/bin with \\\"quote\\\\slash/$binary\""
        elif [ "$os" = MINGW64_NT ]; then
            encoded='"C:\\synthetic\\trello-mcp.exe"'
        else
            encoded="\"$destination/$binary\""
        fi
        grep -Fxq "      \"command\": $encoded" "$run/out"
        grep -Fxq "command = $encoded" "$run/out"
        # Assert the literal path printed for macOS, without expanding its tilde.
        # shellcheck disable=SC2088
        case "$os" in
            Darwin) grep -Fq '~/Library/Application Support/Claude/claude_desktop_config.json' "$run/out" ;;
            MINGW*|MSYS*|CYGWIN*) grep -Fq '%APPDATA%\Claude\claude_desktop_config.json' "$run/out" ;;
            *) grep -Fq 'Claude Desktop has no official Linux build' "$run/out" ;;
        esac
        grep -q 'not on PATH' "$run/err"
        sed -n '/^claude mcp add /p' "$run/out" > "$run/registration.sh"
        sh -n "$run/registration.sh"
        if [ "$mode" = missing ] || [ "$mode" = no-release ] || [ "$os" = FreeBSD ]; then
            grep -q '^go install .*@latest$' "$run/log"
        else
            cmp "$tmp/source/$binary" "$destination/$binary"
            assert_not_logged '^go ' "$run/log"
        fi
    else
        [ ! -e "$destination/$binary" ]
        assert_not_logged '^go ' "$run/log"
    fi
    assert_not_logged '^ERROR:' "$run/log"
    printf 'PASS %s\n' "$name"
}

run_case latest success Linux x86_64 '' pass
run_case pinned success Linux x86_64 v1.2.3 pass
assert_not_logged '/releases/latest' "$tmp/runs/pinned/log"
grep -q '/download/v1.2.3/' "$tmp/runs/pinned/log"
run_case normalized-version success Linux x86_64 1.2.3 pass
run_case escaping success Linux x86_64 v1.2.3 pass
run_case macos success Darwin aarch64 '' pass
run_case missing-asset missing Linux x86_64 v1.2.3 pass
run_case no-release no-release Linux x86_64 '' pass
run_case unsupported success FreeBSD x86_64 '' pass
run_case checksum-mismatch corrupt Linux x86_64 '' fail
grep -q 'checksum verification failed' "$tmp/runs/checksum-mismatch/err"
run_case missing-checksums missing-checksums Linux x86_64 '' fail
run_case invalid-checksum bad-manifest Linux x86_64 '' fail
run_case duplicate-checksum duplicate Linux x86_64 '' fail
run_case forbidden forbidden Linux x86_64 '' fail
run_case network network Linux x86_64 '' fail
run_case invalid-version success Linux x86_64 '../../bad' fail
run_case no-go missing Linux x86_64 v1.2.3 fail "$tmp/no-go"
grep -q 'install Go to build from source' "$tmp/runs/no-go/err"
run_case no-hash success Linux x86_64 v1.2.3 fail "$tmp/no-hash"
grep -q 'sha256sum or shasum is required' "$tmp/runs/no-hash/err"
if command -v shasum >/dev/null 2>&1; then
    run_case shasum success Linux x86_64 v1.2.3 pass "$tmp/shasum"
else
    printf 'SKIP shasum: not installed\n'
fi
if [ "$windows" = true ]; then
    run_case windows success MINGW64_NT x86_64 '' pass
else
    printf 'SKIP Windows archive: zip or unzip not installed\n'
fi
