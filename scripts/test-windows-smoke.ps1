[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [string]$Executable = (Join-Path $PSScriptRoot "..\hearth.exe"),

    [Parameter(Mandatory = $false)]
    [string]$ExpectedVersion = ((Get-Content -LiteralPath (Join-Path $PSScriptRoot "..\VERSION") -Raw).Trim())
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$resolvedExecutable = (Resolve-Path -LiteralPath $Executable).Path
$testRoot = Join-Path $env:RUNNER_TEMP ("hearth-windows-smoke-" + [guid]::NewGuid().ToString("N"))
$configPath = Join-Path $testRoot "config.json"
$passwordPath = Join-Path $testRoot "admin-password.txt"
$logPath = Join-Path $testRoot "hearth.log"
$listenAddress = "127.0.0.1:18080"
$baseUrl = "http://$listenAddress"
$adminPassword = "runner-admin-secret"

[void](New-Item -ItemType Directory -Path $testRoot)
[System.IO.File]::WriteAllText($passwordPath, $adminPassword, [System.Text.UTF8Encoding]::new($false))

$config = [ordered]@{
    listen = $listenAddress
    demo = $false
    secureCookies = $false
    adminPasswordFile = $passwordPath
    accessFile = (Join-Path $testRoot "member-credentials.json")
    auditFile = (Join-Path $testRoot "login-audit.jsonl")
    configAuditFile = (Join-Path $testRoot "config-audit.jsonl")
    operationAuditFile = (Join-Path $testRoot "operation-audit.jsonl")
    ipRulesFile = (Join-Path $testRoot "ip-rules.json")
    deviceKeyFile = (Join-Path $testRoot "device-cookie.key")
    trustedProxyCidrs = @("127.0.0.0/8", "::1/128")
    management = [ordered]@{
        installRoot = (Join-Path $testRoot "game-servers")
        steamCmdRoot = (Join-Path $testRoot "steamcmd")
        discoveryRoots = @((Join-Path $testRoot "discovery"))
    }
    games = [ordered]@{
        palworld = [ordered]@{
            enabled = $false
            installDir = ""
            steamCmd = ""
        }
        dontStarveTogether = [ordered]@{
            enabled = $false
            installDir = ""
            steamCmd = ""
        }
    }
}
[System.IO.File]::WriteAllText(
    $configPath,
    ($config | ConvertTo-Json -Depth 8),
    [System.Text.UTF8Encoding]::new($false)
)

$startInfo = [System.Diagnostics.ProcessStartInfo]::new()
$startInfo.FileName = $resolvedExecutable
$startInfo.UseShellExecute = $false
# ProcessStartInfo.ArgumentList is unavailable in Windows PowerShell 5.1.
# These generated Windows paths cannot contain double quotes, so an explicitly
# quoted argument string remains safe and works on both PowerShell 5.1 and 7.
$startInfo.Arguments = '-config "' + $configPath + '" -log "' + $logPath + '"'
$process = [System.Diagnostics.Process]::Start($startInfo)

try {
    $health = $null
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        if ($process.HasExited) {
            $log = if (Test-Path -LiteralPath $logPath) { Get-Content -LiteralPath $logPath -Raw } else { "<no log>" }
            throw "Hearth exited before becoming healthy (exit $($process.ExitCode)). Log:`n$log"
        }
        try {
            $health = Invoke-RestMethod -Method Get -Uri "$baseUrl/api/v1/health" -TimeoutSec 2
            break
        }
        catch {
            Start-Sleep -Milliseconds 250
        }
    }
    if ($null -eq $health -or $health.status -ne "ok") {
        throw "Hearth did not become healthy within 30 seconds"
    }
    if ($health.version -ne $ExpectedVersion) {
        throw "Health version is '$($health.version)', expected '$ExpectedVersion'"
    }

    $webSession = [Microsoft.PowerShell.Commands.WebRequestSession]::new()
    $loginBody = @{ password = $adminPassword } | ConvertTo-Json
    $session = Invoke-RestMethod `
        -Method Post `
        -Uri "$baseUrl/api/v1/session" `
        -ContentType "application/json" `
        -Body $loginBody `
        -WebSession $webSession
    if (-not $session.authenticated -or $session.role -ne "admin") {
        throw "Administrator login did not return an authenticated admin session"
    }

    $management = Invoke-RestMethod `
        -Method Get `
        -Uri "$baseUrl/api/v1/system/management" `
        -WebSession $webSession
    $palworld = @($management.games | Where-Object { $_.id -eq "palworld" })[0]
    $dontStarveTogether = @($management.games | Where-Object { $_.id -eq "dont-starve-together" })[0]
    if ($palworld.state -ne "not_installed" -or -not $palworld.canInstall) {
        throw "Unexpected Palworld first-start state: $($palworld | ConvertTo-Json -Compress)"
    }
    if ($dontStarveTogether.support -ne "planned") {
        throw "Unexpected Don't Starve Together support state: $($dontStarveTogether | ConvertTo-Json -Compress)"
    }

    Write-Host "Windows first-start smoke test passed."
}
finally {
    if ($null -ne $process -and -not $process.HasExited) {
        # The smoke server does not launch child processes. Parameterless Kill
        # keeps cleanup compatible with Windows PowerShell 5.1 and PowerShell 7.
        $process.Kill()
        $process.WaitForExit(5000)
    }
}
