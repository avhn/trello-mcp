# Offline rollback checks. The callback never downloads or runs an installer.
$ErrorActionPreference = 'Stop'
$hadOriginalPin = Test-Path Env:TRELLO_MCP_VERSION
$originalPin = $env:TRELLO_MCP_VERSION

function Assert-True($condition, $message) {
    if (-not $condition) { throw "FAIL: $message" }
}

try {
    foreach ($existing in @($false, $true)) {
        foreach ($fails in @($false, $true)) {
            if ($existing) {
                $env:TRELLO_MCP_VERSION = 'v0.5.0'
            } else {
                Remove-Item Env:TRELLO_MCP_VERSION -ErrorAction SilentlyContinue
            }
            $script:installerCalls = 0
            $installer = {
                $script:installerCalls++
                Assert-True ($env:TRELLO_MCP_VERSION -eq 'v0.4.2') 'rollback pin was not passed to installer'
                if ($fails) { throw 'synthetic installer failure' }
                'synthetic installed'
            }
            $failure = $null
            $result = $null
            try {
                # Same compact setup.md expression, with only irm | iex replaced by the callback.
                $result = & { $hadPin = Test-Path Env:TRELLO_MCP_VERSION; $oldPin = $env:TRELLO_MCP_VERSION; try { $env:TRELLO_MCP_VERSION = 'v0.4.2'; & $installer } finally { if ($hadPin) { $env:TRELLO_MCP_VERSION = $oldPin } else { Remove-Item Env:TRELLO_MCP_VERSION -ErrorAction SilentlyContinue } } }
            } catch {
                $failure = $_.Exception.Message
            }
            Assert-True ($script:installerCalls -eq 1) 'installer callback must run exactly once'
            if ($fails) {
                Assert-True ($failure -eq 'synthetic installer failure') "wrong installer failure: $failure"
            } else {
                Assert-True (-not $failure -and $result -eq 'synthetic installed') 'successful installer result was not preserved'
            }
            Assert-True ((Test-Path Env:TRELLO_MCP_VERSION) -eq $existing) 'rollback changed whether the pin exists'
            if ($existing) {
                Assert-True ($env:TRELLO_MCP_VERSION -eq 'v0.5.0') 'rollback did not restore the existing pin'
            }
            Write-Output "PASS rollback existing=$existing failure=$fails"
        }
    }
} finally {
    if ($hadOriginalPin) {
        $env:TRELLO_MCP_VERSION = $originalPin
    } else {
        Remove-Item Env:TRELLO_MCP_VERSION -ErrorAction SilentlyContinue
    }
}
