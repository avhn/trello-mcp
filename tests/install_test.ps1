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
    if ($script:missingRelease) { throw 'No published release is available.' }
    return [pscustomobject]@{ tag_name = 'v1.2.3' }
}
function go {
    $script:goCalls++
    throw 'FAIL: installer attempted an automatic source build'
}
function Invoke-WebRequest {
    $script:networkCalls++
    throw 'FAIL: installer attempted network access'
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
    Assert-True ($script:goCalls -eq 0) 'installer attempted a source build'
    Assert-True ($script:networkCalls -eq 0) 'installer attempted real network access'
    Assert-True (-not (Test-Path -LiteralPath $env:INSTALL_DIR)) 'failed install created a destination'
    Assert-True (@(Get-ChildItem -LiteralPath $env:TEMP -Force).Count -eq 0) 'failed install left temporary files'
}

$script:goCalls = 0
$script:networkCalls = 0
try {
    $source = Join-Path $temp 'source'
    $fixtures = Join-Path $temp 'fixtures'
    $release = Join-Path $fixtures 'v1.2.3'
    $runTemp = Join-Path $temp 'runs'
    New-Item -ItemType Directory -Path $source, $release, $runTemp | Out-Null
    # Run the installer text with command sentinels in a fresh PowerShell process.
    # This covers native -File argument forwarding as well as isolated iex usage.
    $sentinelScript = Join-Path $temp 'arguments.ps1'
    $sentinelLog = Join-Path $temp 'arguments.log'
    $prefix = @'
$ErrorActionPreference = 'Stop'
$beforeTls = [Net.ServicePointManager]::SecurityProtocol
$beforePath = $env:PATH
function Block-InstallerEffect {
    [IO.File]::AppendAllText($env:TEST_INSTALL_LOG, "unexpected side effect`n")
    throw 'unexpected installer side effect'
}
function Invoke-RestMethod { Block-InstallerEffect }
function Invoke-WebRequest { Block-InstallerEffect }
function New-Item { Block-InstallerEffect }
function New-ItemProperty { Block-InstallerEffect }
function Get-Item { Block-InstallerEffect }
function Copy-Item { Block-InstallerEffect }
function Expand-Archive { Block-InstallerEffect }
function Get-FileHash { Block-InstallerEffect }
function Join-Path { Block-InstallerEffect }
function go { Block-InstallerEffect }
'@
    $suffix = @'
if ([Net.ServicePointManager]::SecurityProtocol -ne $beforeTls -or $env:PATH -ne $beforePath) {
    throw 'help changed TLS or process PATH'
}
Write-Output 'CALLER_ALIVE'
'@
    $engine = (Get-Process -Id $PID).Path
    $savedTestLog = $env:TEST_INSTALL_LOG
    $savedCredentialPath = $env:TRELLO_MCP_CREDENTIALS
    try {
        $env:TEST_INSTALL_LOG = $sentinelLog
        $env:TRELLO_MCP_CREDENTIALS = 'invalid-relative-test-path'
        $env:INSTALL_DIR = Join-Path $temp 'must-not-exist'
        $env:PROCESSOR_ARCHITECTURE = 'invalid-test-platform'
        foreach ($invocation in @('File', 'Expression')) {
            $body = $installer
            if ($invocation -eq 'Expression') {
                # Escape quotes instead of nesting the installer's own here strings.
                $body = "Invoke-Expression '" + $installer.Replace("'", "''") + "'"
            }
            Set-Content -LiteralPath $sentinelScript -Value ($prefix + "`n" + $body + "`n" + $suffix) -Encoding UTF8
            foreach ($argument in @('-Help', '--help', '-h', '--unknown', 'unknown')) {
                # Capture native stderr without PowerShell 5.1 NativeCommandError handling.
                $process = New-Object Diagnostics.Process
                $process.StartInfo.FileName = $engine
                $process.StartInfo.Arguments = '-NoProfile -File "' + $sentinelScript + '" ' + $argument
                $process.StartInfo.UseShellExecute = $false
                $process.StartInfo.RedirectStandardOutput = $true
                $process.StartInfo.RedirectStandardError = $true
                $process.StartInfo.RedirectStandardInput = $true
                $process.Start() | Out-Null
                $process.StandardInput.Close()
                $captured = $process.StandardOutput.ReadToEnd() + $process.StandardError.ReadToEnd()
                $process.WaitForExit()
                $status = $process.ExitCode
                $process.Dispose()
                if ($argument -in @('-Help', '--help', '-h')) {
                    Assert-True ($status -eq 0) "$invocation $argument failed: $captured"
                    Assert-True ($captured.Contains('CALLER_ALIVE')) 'help terminated the caller'
                    Assert-True ($captured.Contains('TRELLO_MCP_RELEASE_BASE_URL')) 'help omitted advanced override'
                    Assert-True ($captured.Contains('private terminal (not through an agent)')) 'help omitted auth boundary'
                } else {
                    Assert-True ($status -ne 0 -and $captured.Contains('unknown argument')) "$invocation accepted unknown argument: $captured"
                }
                Assert-True (-not (Test-Path -LiteralPath $sentinelLog)) "$invocation $argument invoked a dependency"
                Assert-True (-not (Test-Path -LiteralPath $env:INSTALL_DIR)) "$invocation $argument wrote an install destination"
                Write-Output "PASS $invocation $argument (no side effects)"
            }
        }
    } finally {
        $env:TEST_INSTALL_LOG = $savedTestLog
        $env:TRELLO_MCP_CREDENTIALS = $savedCredentialPath
    }
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
    Assert-True ($output.Contains('Run trello-mcp auth yourself in a private terminal (not through an agent), then restart or reconnect the MCP client.')) 'missing private-human-terminal auth handoff'
    Assert-True ($output.Contains('TRELLO_MCP_CREDENTIALS')) 'missing credential path guidance'
    Assert-True ($output.Contains('%USERPROFILE%\.secrets\.trello-mcp.secret')) 'missing default credential path'
    Assert-True ($output.Contains('TRELLO_READ_ONLY=1 or trello-mcp auth --read-only')) 'missing read-only opt-in'
    Assert-True (-not ($output.Contains('YOUR_API_KEY') -or $output.Contains('YOUR_TOKEN') -or $output.Contains('--env') -or $output.Contains('mcpServers'))) 'installer must link canonical client setup, not duplicate it'
    $registration = "claude mcp add --scope user --transport stdio trello -- '" + $exe.Replace("'", "''") + "'"
    Assert-True ($output.Contains($registration)) 'missing scoped PowerShell-quoted registration'
    # Parse the printed command using a synthetic function, never a real client.
    function claude {
        Assert-True ($args.Count -eq 9) 'wrong registration argument count'
        Assert-True (($args[0..7] -join ' ') -eq 'mcp add --scope user --transport stdio trello --') 'wrong registration scope or transport'
        Assert-True ($args[8] -eq $exe) 'registration executable did not round trip'
    }
    Invoke-Expression $registration
    Assert-True ($output.Contains('new terminal') -and $output.Contains('fail closed')) 'missing PATH or Windows guidance'
    Assert-True ($output.Contains('https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md')) 'missing canonical setup link'
    Write-Output 'PASS success, normalized tag, configuration escaping and auth guidance'

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

    Set-Content -LiteralPath $manifest -Value $lines -Encoding ASCII
    $env:TRELLO_MCP_VERSION = 'v9.9.9'
    $env:INSTALL_DIR = Join-Path $temp 'missing-pinned'
    $latestBefore = $script:latestCalls
    Expect-Failure 'cmd/trello-mcp@v9.9.9'
    Assert-True ($script:latestCalls -eq $latestBefore) 'missing pinned release became latest'
    Write-Output 'PASS missing pinned release fails closed'

    $env:TRELLO_MCP_VERSION = ''
    $script:missingRelease = $true
    $env:INSTALL_DIR = Join-Path $temp 'missing-latest'
    Expect-Failure 'cmd/trello-mcp@latest'
    $script:missingRelease = $false
    Write-Output 'PASS missing latest release fails closed'

    $env:INSTALL_DIR = Join-Path $temp 'missing-unpinned-asset'
    $env:TRELLO_MCP_VERSION = ''
    $missingAsset = Join-Path $release 'trello-mcp_windows_arm64.zip'
    $savedAsset = Join-Path $temp 'saved-arm64.zip'
    Move-Item -LiteralPath $missingAsset -Destination $savedAsset
    Expect-Failure 'cmd/trello-mcp@v1.2.3'
    Move-Item -LiteralPath $savedAsset -Destination $missingAsset
    Write-Output 'PASS resolved latest missing asset retains selected tag'

    $env:INSTALL_DIR = Join-Path $temp 'unsupported-platform'
    $env:TRELLO_MCP_VERSION = 'v1.2.3'
    $env:PROCESSOR_ARCHITECTURE = 'unsupported'
    $env:PROCESSOR_ARCHITEW6432 = ''
    Expect-Failure 'Unsupported Windows architecture'
    $env:PROCESSOR_ARCHITECTURE = 'AMD64'
    Write-Output 'PASS unsupported platform fails closed'

    $env:INSTALL_DIR = Join-Path $temp 'bad-version'
    $env:TRELLO_MCP_VERSION = '../../bad'
    Expect-Failure 'TRELLO_MCP_VERSION must be a release tag'
    Write-Output 'PASS bad version tag'
} finally {
    foreach ($name in $names) { [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process') }
    [Net.ServicePointManager]::SecurityProtocol = $originalTls
    if (Test-Path -LiteralPath $temp) { Remove-Item -LiteralPath $temp -Recurse -Force }
}
