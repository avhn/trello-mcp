# Offline installer integration tests with synthetic binaries and a fake user PATH.
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$installer = Get-Content -LiteralPath (Join-Path $root 'install.ps1') -Raw
$temp = Join-Path ([IO.Path]::GetTempPath()) ('trello-mcp-test-' + [Guid]::NewGuid().ToString('N'))
$names = @('INSTALL_DIR', 'TRELLO_MCP_VERSION', 'TRELLO_MCP_RELEASE_BASE_URL', 'PROCESSOR_ARCHITECTURE', 'PROCESSOR_ARCHITEW6432', 'TEMP', 'TMP')
$saved = @{}
foreach ($name in $names) { $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
$originalTls = [Net.ServicePointManager]::SecurityProtocol

function Assert-True($condition, $message) {
    if (-not $condition) { throw "FAIL: $message" }
}

# These functions shadow registry cmdlets. No test reads or writes the real user PATH.
function Get-Item {
    [CmdletBinding()]
    param($LiteralPath)
    if ($LiteralPath -eq 'HKCU:\Environment') {
        $key = New-Object PSObject
        $key | Add-Member -MemberType ScriptMethod -Name GetValue -Value {
            param($name, $default, $options)
            Assert-True ($name -eq 'Path' -and $options -eq [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) 'unexpected registry read'
            return $script:fakeUserPath
        }
        return $key
    }
    Assert-True (-not $LiteralPath.StartsWith('HKCU:')) 'unexpected registry key'
    Microsoft.PowerShell.Management\Get-Item -LiteralPath $LiteralPath
}
function Invoke-RestMethod {
    param($Uri)
    Assert-True ($Uri -eq 'https://api.github.com/repos/avhn/trello-mcp/releases/latest') 'unexpected release lookup'
    $script:latestCalls++
    return [pscustomobject]@{ tag_name = 'v1.2.3' }
}
function New-ItemProperty {
    [CmdletBinding()]
    param($LiteralPath, $Name, $Value, $PropertyType, [switch]$Force)
    Assert-True ($LiteralPath -eq 'HKCU:\Environment' -and $Name -eq 'Path') 'unexpected registry write'
    Assert-True ($PropertyType -eq 'ExpandString') 'PATH must preserve expandable entries'
    $script:fakeUserPath = $Value
    $script:pathWrites++
}

function Run-Installer {
    # Exercise the same invocation as irm | iex without downloading or executing the fixture binary.
    $output = (Invoke-Expression $installer | Out-String)
    Assert-True (-not (Get-Variable -Name archiveDir -Scope Local -ErrorAction SilentlyContinue)) 'installer leaked variables'
    Assert-True (@(Get-ChildItem -LiteralPath $env:TEMP -Force).Count -eq 0) 'installer left temporary files'
    return $output
}
function Expect-Failure($message) {
    $failure = $null
    try { Run-Installer | Out-Null } catch { $failure = $_.Exception.Message }
    Assert-True ($failure -and $failure.Contains($message)) "expected failure containing '$message', got '$failure'"
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $env:INSTALL_DIR 'trello-mcp.exe'))) 'failed install wrote a binary'
    Assert-True ($script:pathWrites -eq 0) 'failed install changed PATH'
    Assert-True (@(Get-ChildItem -LiteralPath $env:TEMP -Force).Count -eq 0) 'failed install left temporary files'
}

try {
    $source = Join-Path $temp 'source'
    $fixtures = Join-Path $temp 'fixtures'
    $release = Join-Path $fixtures 'v1.2.3'
    $runTemp = Join-Path $temp 'runs'
    New-Item -ItemType Directory -Path $source, $release, $runTemp | Out-Null
    $synthetic = 'synthetic release binary, never executed'
    $sourceExe = Join-Path $source 'trello-mcp.exe'
    Set-Content -LiteralPath $sourceExe -Value $synthetic -Encoding ASCII
    $manifest = Join-Path $release 'checksums.txt'
    $lines = @()
    foreach ($arch in @('amd64', 'arm64')) {
        $asset = "trello-mcp_windows_$arch.zip"
        $zip = Join-Path $release $asset
        Compress-Archive -LiteralPath $sourceExe -DestinationPath $zip
        $lines += (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant() + '  ' + $asset
    }
    Set-Content -LiteralPath $manifest -Value $lines -Encoding ASCII
    $env:TRELLO_MCP_RELEASE_BASE_URL = $fixtures
    $env:TRELLO_MCP_VERSION = '1.2.3'
    $env:PROCESSOR_ARCHITECTURE = 'AMD64'
    $env:PROCESSOR_ARCHITEW6432 = ''
    $env:TEMP = $runTemp
    $env:TMP = $runTemp
    $env:INSTALL_DIR = Join-Path $temp "bin with 'quote"
    $script:fakeUserPath = '%LOCALAPPDATA%\Existing;C:\Other'
    $script:pathWrites = 0
    $script:latestCalls = 0
    $output = Run-Installer
    $exe = Join-Path $env:INSTALL_DIR 'trello-mcp.exe'
    Assert-True ((Get-Content -LiteralPath $exe -Raw).Trim() -eq $synthetic) 'wrong installed binary'
    Assert-True ($script:pathWrites -eq 1) 'success did not add user PATH once'
    Assert-True ($script:fakeUserPath -eq ('%LOCALAPPDATA%\Existing;C:\Other;' + $env:INSTALL_DIR)) 'existing PATH was not preserved'
    Assert-True ($output.Contains('YOUR_API_KEY') -and $output.Contains('YOUR_TOKEN')) 'missing placeholders'
    Assert-True ($output.Contains('claude mcp add') -and $output.Contains("bin with ''quote")) 'missing PowerShell-quoted registration'
    Assert-True ($output.Contains('new terminal') -and $output.Contains('fail closed')) 'missing PATH or Windows guidance'
    $jsonMatch = [regex]::Match($output, '(?s)\{\s*"mcpServers".*?\r?\n\}')
    Assert-True $jsonMatch.Success 'missing Desktop JSON'
    $config = $jsonMatch.Value | ConvertFrom-Json
    Assert-True ($config.mcpServers.trello.command -eq $exe) 'Desktop path did not round trip as JSON'
    Assert-True ($config.mcpServers.trello.env.TRELLO_READ_ONLY -eq '1') 'missing read-only mode'
    $tomlCommand = [regex]::Match($output, '(?m)^command = (.+)\r?$').Groups[1].Value.Trim()
    Assert-True ($tomlCommand -eq (ConvertTo-Json -InputObject $exe -Compress)) 'Codex command is not an escaped basic string'
    Write-Output 'PASS success, normalized tag, configuration escaping and placeholders'

    Run-Installer | Out-Null
    Assert-True ($script:pathWrites -eq 1) 'reinstall duplicated user PATH'
    $script:fakeUserPath = $env:INSTALL_DIR.ToUpperInvariant() + '\;C:\Other'
    Run-Installer | Out-Null
    Assert-True ($script:pathWrites -eq 1) 'case or trailing separator caused duplicate PATH'
    Write-Output 'PASS PATH idempotency'

    Assert-True ($script:latestCalls -eq 0) 'pinned release performed a latest lookup'
    $env:TRELLO_MCP_VERSION = ''
    Run-Installer | Out-Null
    Assert-True ($script:latestCalls -eq 1) 'latest release was not resolved'
    Write-Output 'PASS latest release lookup (mocked offline)'

    $env:INSTALL_DIR = Join-Path $temp 'arm64'
    $env:PROCESSOR_ARCHITECTURE = 'x86'
    $env:PROCESSOR_ARCHITEW6432 = 'ARM64'
    $env:TRELLO_MCP_VERSION = 'v1.2.3'
    Run-Installer | Out-Null
    Assert-True (Test-Path -LiteralPath (Join-Path $env:INSTALL_DIR 'trello-mcp.exe')) 'ARM64 override not installed'
    Write-Output 'PASS ARM64 native architecture override and pinned tag'

    $script:pathWrites = 0
    $env:INSTALL_DIR = Join-Path $temp 'bad-checksum'
    $badLines = @($lines | ForEach-Object { $_ -replace '^[0-9a-f]{64}', ('0' * 64) })
    Set-Content -LiteralPath $manifest -Value $badLines -Encoding ASCII
    Expect-Failure 'Checksum verification failed'
    Write-Output 'PASS checksum mismatch'

    $env:INSTALL_DIR = Join-Path $temp 'bad-version'
    $env:TRELLO_MCP_VERSION = '../../bad'
    Expect-Failure 'TRELLO_MCP_VERSION must be a release tag'
    Write-Output 'PASS bad version tag'
} finally {
    foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process') }
    [Net.ServicePointManager]::SecurityProtocol = $originalTls
    if (Test-Path -LiteralPath $temp) { Remove-Item -LiteralPath $temp -Recurse -Force }
}
