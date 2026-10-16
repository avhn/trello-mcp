#!/bin/sh
# Offline installer integration tests. Downloads are synthetic; Go must never run.
set -eu
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' 0
trap 'exit 1' HUP INT TERM
mkdir "$tmp/bin" "$tmp/source" "$tmp/archives" "$tmp/checksums" "$tmp/runs"
cat > "$tmp/source/trello-mcp" <<'EOF'
#!/bin/sh
case "$*" in
    --version) printf 'synthetic version\n' ;;
    'auth status') printf 'synthetic status\n' ;;
    'auth logout') printf 'synthetic logout\n' ;;
    *) printf 'FAIL: unexpected synthetic command\n' >&2; exit 1 ;;
esac
EOF
for platform in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
    tar -czf "$tmp/archives/trello-mcp_$platform.tar.gz" -C "$tmp/source" trello-mcp
done
windows=false
if command -v zip >/dev/null 2>&1 && command -v unzip >/dev/null 2>&1; then
    windows=true
    cp "$tmp/source/trello-mcp" "$tmp/source/trello-mcp.exe"
    for arch in amd64 arm64; do
        zip -jq "$tmp/archives/trello-mcp_windows_$arch.zip" "$tmp/source/trello-mcp.exe"
    done
else
    # Unselected Windows archives still belong to the unified checksum fixture.
    for arch in amd64 arm64; do
        printf 'synthetic unselected archive\n' > "$tmp/archives/trello-mcp_windows_$arch.zip"
    done
fi
# Installers select one archive from the normal release's ten-entry checksum list.
for bundle in desktop linux_amd64 linux_arm64; do
    printf 'synthetic unselected bundle\n' > "$tmp/archives/trello-mcp_$bundle.mcpb"
done
printf '{"synthetic":"metadata"}\n' > "$tmp/archives/server.json"
if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp/archives" && sha256sum ./*) > "$tmp/checksums/checksums.txt"
else
    (cd "$tmp/archives" && shasum -a 256 ./*) > "$tmp/checksums/checksums.txt"
fi
# Match the release manifest's bare archive names.
sed 's|  \./|  |' "$tmp/checksums/checksums.txt" > "$tmp/checksums/manifest"
[ "$(wc -l < "$tmp/checksums/manifest" | tr -d ' ')" -eq 10 ]
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
exit 1
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
        grep -Fq 'Run this yourself in a private' "$run/out"
        grep -Fq 'terminal, not through an agent:' "$run/out"
        grep -Fq 'use trello Reconnect in /mcp, or fully quit and restart your MCP client.' "$run/out"
        grep -Fq 'For a read-only token, add the --read-only flag.' "$run/out"
        grep -Fq 'TRELLO_READ_ONLY=1' "$run/out"
        grep -Fq "trello-mcp $( [ -n "$version" ] && printf 'v%s' "${version#v}" || printf 'v1.2.3') installed to " "$run/out"
        # Captured output is not a terminal, so it must carry no styling.
        if grep -q "$(printf '\033')" "$run/out" "$run/err"; then
            printf 'FAIL %s: escape sequences in captured output\n' "$name" >&2
            exit 1
        fi
        grep -Fq 'TRELLO_MCP_CREDENTIALS' "$run/out"
        grep -Fq 'https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md' "$run/out"
        grep -Fq 'claude mcp add --scope user --transport stdio trello -- ' "$run/out"
        assert_not_logged 'YOUR_API_KEY\|YOUR_TOKEN\|--env\|"mcpServers"\|\[mcp_servers' "$run/out"
        grep -q 'is not on your PATH' "$run/err"
        grep -Fq 'The commands below use the full path' "$run/err"
        sed -n 's/^       \(claude mcp add \)/\1/p' "$run/out" > "$run/registration.sh"
        sh -n "$run/registration.sh"
        # Parse and execute only the printed registration with a synthetic function.
        if [ "$os" = MINGW64_NT ]; then
            expected_binary='C:\synthetic\trello-mcp.exe'
        else
            expected_binary="$destination/$binary"
        fi
        EXPECTED_BINARY="$expected_binary" sh -c '
            claude() {
                [ "$#" -eq 9 ] && [ "$1 $2 $3 $4 $5 $6 $7 $8" = "mcp add --scope user --transport stdio trello --" ] && [ "$9" = "$EXPECTED_BINARY" ]
            }
            . "$1"
        ' sh "$run/registration.sh"
        # Parse auth without executing it. Lifecycle commands use only the synthetic binary.
        sed -n '/^       claude /d; s/^       \(.* auth\)$/set -- \1/p' "$run/out" > "$run/auth.sh"
        EXPECTED_BINARY="$expected_binary" sh -c '
            count=0
            while IFS= read -r command; do
                eval "$command"
                [ "$#" -eq 2 ] && [ "$1" = "$EXPECTED_BINARY" ] && [ "$2" = auth ] || exit 1
                count=$((count + 1))
            done < "$1"
            [ "$count" -eq 1 ]
        ' sh "$run/auth.sh"
        if [ "$os" != MINGW64_NT ]; then
            sed -n 's/^  Status    //p; s/^  Version   //p; s/^  Logout    //p' "$run/out" > "$run/lifecycle.sh"
            env -i PATH="$tmp/bin:/usr/bin:/bin" HOME="$run/home" sh "$run/lifecycle.sh" > "$run/lifecycle.out"
            grep -Fxq 'synthetic status' "$run/lifecycle.out"
            grep -Fxq 'synthetic version' "$run/lifecycle.out"
            grep -Fxq 'synthetic logout' "$run/lifecycle.out"
        fi
        cmp "$tmp/source/$binary" "$destination/$binary"
        assert_not_logged '^go ' "$run/log"
    else
        [ ! -e "$destination" ]
        assert_not_logged '^go ' "$run/log"
    fi
    assert_not_logged '^ERROR:' "$run/log"
    printf 'PASS %s\n' "$name"
}

# Help and invalid arguments must not invoke dependency/platform/temp/PATH tools.
mkdir "$tmp/sentinels"
for tool in curl uname mktemp go mkdir cp chmod claude cygpath; do
    cat > "$tmp/sentinels/$tool" <<'EOF'
#!/bin/sh
printf 'ERROR: side effect %s\n' "$0" >> "$TEST_LOG"
exit 1
EOF
    chmod +x "$tmp/sentinels/$tool"
done
shell=$(command -v sh)
for mode in --help -h --unknown unknown; do
    log="$tmp/runs/arguments.log"
    : > "$log"
    if env -i PATH="$tmp/sentinels:/usr/bin:/bin" HOME="$tmp/absent-home" \
        INSTALL_DIR="$tmp/absent-install" TMPDIR="$tmp/absent-temp" \
        TRELLO_MCP_CREDENTIALS=relative-invalid TEST_LOG="$log" \
        "$shell" "$root/install.sh" "$mode" < /dev/null > "$tmp/runs/arguments.out" 2> "$tmp/runs/arguments.err"; then
        case "$mode" in --help|-h) ;; *) printf 'FAIL accepted %s\n' "$mode"; exit 1 ;; esac
        grep -Fq 'TRELLO_MCP_VERSION' "$tmp/runs/arguments.out"
        grep -Fq 'Run the printed absolute-path auth command yourself in a private terminal (not through an agent), then use trello Reconnect in /mcp or fully quit/restart the MCP client.' "$tmp/runs/arguments.out"
    else
        case "$mode" in --help|-h) cat "$tmp/runs/arguments.err"; exit 1 ;; esac
        grep -Fq 'unknown argument' "$tmp/runs/arguments.err"
    fi
    [ ! -s "$log" ]
    [ ! -e "$tmp/absent-home" ] && [ ! -e "$tmp/absent-install" ] && [ ! -e "$tmp/absent-temp" ]
    printf 'PASS argument %s (no side effects)\n' "$mode"
done

run_case latest success Linux x86_64 '' pass
run_case pinned success Linux x86_64 v1.2.3 pass
assert_not_logged '/releases/latest' "$tmp/runs/pinned/log"
grep -q '/download/v1.2.3/' "$tmp/runs/pinned/log"
run_case normalized-version success Linux x86_64 1.2.3 pass
run_case escaping success Linux x86_64 v1.2.3 pass
run_case macos success Darwin aarch64 '' pass
run_case missing-asset missing Linux x86_64 v1.2.3 fail
assert_not_logged '/releases/latest' "$tmp/runs/missing-asset/log"
grep -Fq 'cmd/trello-mcp@v1.2.3' "$tmp/runs/missing-asset/err"
run_case missing-unpinned missing Linux x86_64 '' fail
grep -Fq 'cmd/trello-mcp@v1.2.3' "$tmp/runs/missing-unpinned/err"
run_case no-release no-release Linux x86_64 '' fail
grep -Fq 'cmd/trello-mcp@latest' "$tmp/runs/no-release/err"
run_case unsupported success FreeBSD x86_64 '' fail
assert_not_logged '/releases/' "$tmp/runs/unsupported/log"
run_case unsupported-pinned success FreeBSD x86_64 v1.2.3 fail
grep -Fq 'cmd/trello-mcp@v1.2.3' "$tmp/runs/unsupported-pinned/err"
run_case checksum-mismatch corrupt Linux x86_64 '' fail
grep -q 'checksum verification failed' "$tmp/runs/checksum-mismatch/err"
run_case missing-checksums missing-checksums Linux x86_64 '' fail
run_case invalid-checksum bad-manifest Linux x86_64 '' fail
run_case duplicate-checksum duplicate Linux x86_64 '' fail
run_case forbidden forbidden Linux x86_64 '' fail
run_case network network Linux x86_64 '' fail
run_case invalid-version success Linux x86_64 '../../bad' fail
run_case no-go missing Linux x86_64 v1.2.3 fail "$tmp/no-go"
grep -q 'no automatic source build was attempted' "$tmp/runs/no-go/err"
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
