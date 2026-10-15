# Install a verified release without reading or configuring Trello credentials.
# Keep variables and helper functions local when invoked through irm | iex.
& {
    $ErrorActionPreference = 'Stop'
    $showHelp = $false
    foreach ($argument in $args) {
        switch -Exact ($argument) {
            '-Help' { $showHelp = $true }
            '--help' { $showHelp = $true }
            '-h' { $showHelp = $true }
            default { throw "trello-mcp: unknown argument: $argument (use -Help)" }
        }
    }
    if ($showHelp) {
        Write-Output @'
Usage: install.ps1 [-Help|--help|-h]
Install a checksum-verified release into INSTALL_DIR (default: %LOCALAPPDATA%\Programs\trello-mcp).
TRELLO_MCP_VERSION selects an exact release tag; omitted means latest release.
Downloads and SHA-256 checksums must succeed; no automatic source build occurs.
Advanced/test override: TRELLO_MCP_RELEASE_BASE_URL accepts HTTPS or an absolute fixture directory.
Pin TRELLO_MCP_VERSION when using offline fixtures to avoid latest-release networking.
Installation adds only the destination to the user PATH; no client configuration is changed.
Run trello-mcp auth yourself in a private terminal (not through an agent), then restart or reconnect the MCP client.
Setup: https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md
'@
        return
    }
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    $temp = $null
    $tag = $env:TRELLO_MCP_VERSION
    try {
        $architecture = $env:PROCESSOR_ARCHITECTURE
        if ($env:PROCESSOR_ARCHITEW6432) { $architecture = $env:PROCESSOR_ARCHITEW6432 }
        switch ($architecture) {
            'AMD64' { $arch = 'amd64' }
            'ARM64' { $arch = 'arm64' }
            default { throw "Unsupported Windows architecture: $architecture (expected AMD64 or ARM64)." }
        }

        if (-not $tag) {
            try {
                $release = Invoke-RestMethod -Uri 'https://api.github.com/repos/avhn/trello-mcp/releases/latest'
                $tag = [string]$release.tag_name
                if (-not $tag) { throw 'No release tag returned.' }
            } catch { throw "Could not resolve the latest release: $($_.Exception.Message)" }
        }
        if (-not $tag.StartsWith('v', [StringComparison]::Ordinal)) { $tag = "v$tag" }
        if ($tag -cnotmatch '\Av[a-zA-Z0-9._-]+\z') {
            throw 'TRELLO_MCP_VERSION must be a release tag such as v0.1.0.'
        }

        $installDir = $env:INSTALL_DIR
        if (-not $installDir) {
            if (-not $env:LOCALAPPDATA) { throw 'LOCALAPPDATA is not set; specify INSTALL_DIR.' }
            $installDir = Join-Path $env:LOCALAPPDATA 'Programs\trello-mcp'
        }
        $installDir = [IO.Path]::GetFullPath($installDir)
        $exe = Join-Path $installDir 'trello-mcp.exe'
        $asset = "trello-mcp_windows_$arch.zip"
        # Override the download root with HTTPS or an absolute fixture directory.
        # Local fixtures require TRELLO_MCP_VERSION to avoid latest-release networking.
        $base = $env:TRELLO_MCP_RELEASE_BASE_URL
        if (-not $base) { $base = 'https://github.com/avhn/trello-mcp/releases/download' }
        function Save-ReleaseFile($name, $destination) {
            try {
                if ($base -match '\Ahttps://') {
                    Invoke-WebRequest -UseBasicParsing -Uri "$($base.TrimEnd('/'))/$tag/$name" -OutFile $destination
                } elseif ([IO.Path]::IsPathRooted($base)) {
                    Copy-Item -LiteralPath (Join-Path (Join-Path $base $tag) $name) -Destination $destination
                } else { throw 'TRELLO_MCP_RELEASE_BASE_URL must be HTTPS or an absolute fixture directory.' }
            } catch { throw "Could not download ${name}: $($_.Exception.Message)" }
        }

        $temp = Join-Path ([IO.Path]::GetTempPath()) ("trello-mcp-" + [Guid]::NewGuid().ToString('N'))
        # Downloads and extraction each get a separate, new directory.
        $archiveDir = Join-Path $temp 'archive'
        $checksumDir = Join-Path $temp 'checksums'
        $unpackDir = Join-Path $temp 'unpack'
        New-Item -ItemType Directory -Path $archiveDir, $checksumDir, $unpackDir | Out-Null
        $archive = Join-Path $archiveDir $asset
        $manifest = Join-Path $checksumDir 'checksums.txt'
        Save-ReleaseFile $asset $archive
        Save-ReleaseFile 'checksums.txt' $manifest
        $entries = @(Get-Content -LiteralPath $manifest | Where-Object {
            $_ -cmatch ('\A\S+\s+\*?' + [regex]::Escape($asset) + '\z')
        })
        if ($entries.Count -ne 1) { throw "Missing or duplicate checksum for $asset." }
        $expected = ($entries[0] -split '\s+')[0]
        if ($expected -cnotmatch '\A[0-9a-f]{64}\z') { throw "Invalid checksum for $asset." }
        $actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash
        if ($actual -ine $expected) { throw "Checksum verification failed for $asset." }
        Expand-Archive -LiteralPath $archive -DestinationPath $unpackDir
        $binary = Join-Path $unpackDir 'trello-mcp.exe'
        $file = Get-Item -LiteralPath $binary
        if ($file.PSIsContainer -or ($file.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
            throw 'Archive does not contain a regular binary.'
        }
        New-Item -ItemType Directory -Path $installDir -Force | Out-Null
        Copy-Item -LiteralPath $binary -Destination $exe -Force

        # Read and update only the user registry PATH, never the merged process PATH.
        $userEnvironment = Get-Item -LiteralPath 'HKCU:\Environment'
        $userPath = [string]$userEnvironment.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        $present = @($userPath -split ';' | Where-Object {
            [Environment]::ExpandEnvironmentVariables($_).TrimEnd('\') -ieq $installDir.TrimEnd('\')
        }).Count -gt 0
        if (-not $present) {
            $newPath = $installDir
            if ($userPath) { $newPath = $userPath.TrimEnd(';') + ';' + $installDir }
            New-ItemProperty -LiteralPath 'HKCU:\Environment' -Name Path -Value $newPath -PropertyType ExpandString -Force | Out-Null
            Write-Output 'Added the install directory to your user PATH.'
        }
        Write-Output "Installed $exe"
        Write-Output 'Open a new terminal to use the updated user PATH. MCP clients can use the full path below.'
        Write-Output 'Windows refuses path uploads and attachment downloads (fail closed). Base64 uploads remain available.'

        $quotedExe = "'" + $exe.Replace("'", "''") + "'"
        Write-Output @"

Run trello-mcp auth yourself in a private terminal (not through an agent), then restart or reconnect the MCP client.
Installed executable: $exe
Credentials: %USERPROFILE%\.secrets\.trello-mcp.secret; override with absolute TRELLO_MCP_CREDENTIALS or auth --file PATH.
Read-only opt-in: TRELLO_READ_ONLY=1 or trello-mcp auth --read-only.

Register with Claude Code (user scope, in PowerShell):
claude mcp add --scope user --transport stdio trello -- $quotedExe

Setup, other clients and explicit local/project scope alternatives:
https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md
"@
    } catch {
        $sourceVersion = 'latest'
        if ($tag -cmatch '\Av?[a-zA-Z0-9._-]+\z') {
            $sourceVersion = $tag
            if (-not $sourceVersion.StartsWith('v', [StringComparison]::Ordinal)) { $sourceVersion = "v$sourceVersion" }
        }
        throw "trello-mcp: $($_.Exception.Message) No automatic source build was attempted. Manual source-build alternative (not executed): go install github.com/avhn/trello-mcp/cmd/trello-mcp@$sourceVersion"
    } finally {
        if ($temp -and (Test-Path -LiteralPath $temp)) {
            Remove-Item -LiteralPath $temp -Recurse -Force
        }
    }
} @args
