<#
.SYNOPSIS
Installs the panel as a Windows startup task without touching the running game.

.DESCRIPTION
The installer copies only the panel binary, writes its private configuration,
and registers a SYSTEM scheduled task. It does not stop, restart, update, or
edit Palworld, and it does not open a firewall port.
#>

[CmdletBinding()]
param(
    [Parameter()]
    [string]$SourceDirectory = $PSScriptRoot,

    [Parameter()]
    [string]$SteamCmdRoot = (Join-Path $env:USERPROFILE 'Downloads\steamcmd'),

    [Parameter()]
    [switch]$Force
)

#Requires -RunAsAdministrator

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$taskName = 'Hearth'
$minimumPanelPasswordLength = 10
$installRoot = Join-Path $env:ProgramData $taskName
$sourceExecutable = Join-Path $SourceDirectory 'hearth.exe'
$sourceUpdater = Join-Path $SourceDirectory 'hearth-updater.exe'
$versionFile = Join-Path $SourceDirectory 'VERSION'
$targetExecutable = Join-Path $installRoot 'hearth.exe'
$targetUpdater = Join-Path $installRoot 'hearth-updater.exe'
$configPath = Join-Path $installRoot 'config.json'
$passwordPath = Join-Path $installRoot 'admin-password.txt'
$accessPath = Join-Path $installRoot 'member-credentials.json'
$auditPath = Join-Path $installRoot 'login-audit.jsonl'
$configAuditPath = Join-Path $installRoot 'config-audit.jsonl'
$operationAuditPath = Join-Path $installRoot 'operation-audit.jsonl'
$ipRulesPath = Join-Path $installRoot 'ip-rules.json'
$deviceKeyPath = Join-Path $installRoot 'device-cookie.key'
$logPath = Join-Path $installRoot 'panel.log'
$palworldRoot = Join-Path $SteamCmdRoot 'steamapps\common\PalServer'
$steamCmd = Join-Path $SteamCmdRoot 'steamcmd.exe'
$settingsFile = Join-Path $palworldRoot 'Pal\Saved\Config\WindowsServer\PalWorldSettings.ini'
$defaultSettingsFile = Join-Path $palworldRoot 'DefaultPalWorldSettings.ini'
$palworldExecutable = Join-Path $palworldRoot 'Pal\Binaries\Win64\PalServer-Win64-Shipping-Cmd.exe'

foreach ($requiredPath in @($sourceExecutable, $sourceUpdater, $versionFile)) {
    if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
        throw "Required file was not found: $requiredPath"
    }
}

$expectedVersion = [IO.File]::ReadAllText($versionFile).Trim()
if ([string]::IsNullOrWhiteSpace($expectedVersion)) {
    throw "Package VERSION is empty: $versionFile"
}

$existingTask = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($null -ne $existingTask -and -not $Force) {
    throw "Scheduled task '$taskName' already exists. Re-run with -Force only when intentionally upgrading Hearth."
}

$preserveConfiguration = $Force -and (Test-Path -LiteralPath $configPath -PathType Leaf)
$effectivePasswordPath = $passwordPath
if ($preserveConfiguration) {
    try {
        $existingConfiguration = [IO.File]::ReadAllText($configPath) | ConvertFrom-Json
        $adminPasswordProperty = $existingConfiguration.PSObject.Properties['adminPasswordFile']
        if ($null -ne $adminPasswordProperty -and -not [string]::IsNullOrWhiteSpace([string]$adminPasswordProperty.Value)) {
            $effectivePasswordPath = [string]$adminPasswordProperty.Value
        }
        $gamesProperty = $existingConfiguration.PSObject.Properties['games']
        $palworldProperty = if ($null -ne $gamesProperty) { $gamesProperty.Value.PSObject.Properties['palworld'] } else { $null }
        if ($null -ne $palworldProperty) {
            $existingPalworld = $palworldProperty.Value
            $pathMappings = @{
                installDir = 'palworldRoot'; steamCmd = 'steamCmd'; settingsFile = 'settingsFile'
                defaultSettingsFile = 'defaultSettingsFile'; executable = 'palworldExecutable'
            }
            foreach ($propertyName in $pathMappings.Keys) {
                $property = $existingPalworld.PSObject.Properties[$propertyName]
                if ($null -eq $property -or [string]::IsNullOrWhiteSpace([string]$property.Value)) { continue }
                Set-Variable -Name $pathMappings[$propertyName] -Value ([string]$property.Value)
            }
        }
    }
    catch {
        throw "Existing Hearth configuration is invalid; upgrade stopped without replacing it: $($_.Exception.Message)"
    }
}

$palworldReady = @($steamCmd, $settingsFile, $defaultSettingsFile, $palworldExecutable) |
    ForEach-Object { Test-Path -LiteralPath $_ -PathType Leaf } |
    Where-Object { -not $_ } |
    Measure-Object |
    Select-Object -ExpandProperty Count
$palworldReady = ($palworldReady -eq 0)

Write-Host 'Set the web panel administrator password. This is separate from the Palworld AdminPassword.'
$securePassword = Read-Host -AsSecureString 'Panel administrator password'
$passwordPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($securePassword)
try {
    $plainPassword = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($passwordPointer)
    if ([string]::IsNullOrWhiteSpace($plainPassword) -or $plainPassword.Length -lt $minimumPanelPasswordLength) {
        throw "Panel administrator password must contain at least $minimumPanelPasswordLength characters."
    }

    New-Item -ItemType Directory -Path $installRoot -Force | Out-Null
    $executableBackupPath = $null
    $updaterBackupPath = $null
    if ($Force -and (Test-Path -LiteralPath $configPath -PathType Leaf)) {
        $backupName = 'config.json.install-backup-{0}' -f (Get-Date -Format 'yyyyMMdd-HHmmss')
        Copy-Item -LiteralPath $configPath -Destination (Join-Path $installRoot $backupName) -Force
    }
    if ($null -ne $existingTask -and (Test-Path -LiteralPath $targetExecutable -PathType Leaf)) {
        $executableBackupPath = Join-Path $installRoot 'hearth.exe.install-backup'
        Copy-Item -LiteralPath $targetExecutable -Destination $executableBackupPath -Force
    }
    if ($null -ne $existingTask -and (Test-Path -LiteralPath $targetUpdater -PathType Leaf)) {
        $updaterBackupPath = Join-Path $installRoot 'hearth-updater.exe.install-backup'
        Copy-Item -LiteralPath $targetUpdater -Destination $updaterBackupPath -Force
    }

    if ($null -ne $existingTask) {
        Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
        $stopDeadline = (Get-Date).AddSeconds(15)
        do {
            Start-Sleep -Milliseconds 250
            $existingProcess = Get-Process -Name 'hearth' -ErrorAction SilentlyContinue
        } while ($null -ne $existingProcess -and (Get-Date) -lt $stopDeadline)
        if ($null -ne $existingProcess) {
            Start-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
            throw 'The existing Hearth process did not stop within 15 seconds. The game server was not changed.'
        }
    }

    try {
        Copy-Item -LiteralPath $sourceExecutable -Destination $targetExecutable -Force
        Copy-Item -LiteralPath $sourceUpdater -Destination $targetUpdater -Force
    }
    catch {
        if ($null -ne $executableBackupPath -and (Test-Path -LiteralPath $executableBackupPath -PathType Leaf)) {
            Copy-Item -LiteralPath $executableBackupPath -Destination $targetExecutable -Force
        }
        if ($null -ne $updaterBackupPath -and (Test-Path -LiteralPath $updaterBackupPath -PathType Leaf)) {
            Copy-Item -LiteralPath $updaterBackupPath -Destination $targetUpdater -Force
        }
        if ($null -ne $existingTask) {
            Start-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
        }
        throw
    }

    $utf8WithoutBom = New-Object System.Text.UTF8Encoding($false)
    $passwordDirectory = Split-Path -Parent $effectivePasswordPath
    if (-not [string]::IsNullOrWhiteSpace($passwordDirectory)) {
        New-Item -ItemType Directory -Path $passwordDirectory -Force | Out-Null
    }
    [IO.File]::WriteAllText($effectivePasswordPath, $plainPassword, $utf8WithoutBom)

    $configuration = [ordered]@{
        listen            = '127.0.0.1:8080'
        demo              = $false
        secureCookies     = $false
        adminPasswordFile = $passwordPath
        accessFile        = $accessPath
        auditFile         = $auditPath
        configAuditFile   = $configAuditPath
        operationAuditFile = $operationAuditPath
        ipRulesFile       = $ipRulesPath
        deviceKeyFile     = $deviceKeyPath
        trustedProxyCidrs = @('127.0.0.0/8', '::1/128')
        management        = [ordered]@{
            installRoot   = (Join-Path $SteamCmdRoot 'steamapps\common')
            steamCmdRoot  = $SteamCmdRoot
            discoveryRoots = @($SteamCmdRoot)
        }
        update            = [ordered]@{
            channel    = 'stable'
            tokenFile  = (Join-Path $installRoot 'github-token.txt')
            stagingDir = (Join-Path $installRoot 'updates')
        }
        games             = [ordered]@{
            palworld = [ordered]@{
                enabled             = $palworldReady
                installDir          = $palworldRoot
                steamCmd            = $steamCmd
                settingsFile        = $settingsFile
                defaultSettingsFile = $defaultSettingsFile
                executable          = $palworldExecutable
                processName         = 'PalServer-Win64-Shipping-Cmd.exe'
                startArgs           = @('-useperfthreads', '-NoAsyncLoadingThread', '-UseMultithreadForDS')
                backupDir           = (Join-Path $palworldRoot 'panel-backups')
                backupRetentionDays = 30
                backupMaxTotalGB    = 20
                restUrl             = 'http://127.0.0.1:8212'
                restUsername        = 'admin'
                shutdownWaitSeconds = 30
                steamCmdNoProgressMinutes = 30
                port                = 8211
            }
            dontStarveTogether = [ordered]@{
                enabled    = $false
                installDir = ''
                steamCmd   = ''
            }
        }
    }
    if (-not $preserveConfiguration) {
        [IO.File]::WriteAllText($configPath, ($configuration | ConvertTo-Json -Depth 8), $utf8WithoutBom)
    }
    else {
        Write-Host 'Existing config.json was preserved. New optional fields will use backward-compatible defaults.'
    }
}
finally {
    if ($passwordPointer -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($passwordPointer)
    }
    $plainPassword = $null
}

& icacls.exe $installRoot /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null

$action = New-ScheduledTaskAction `
    -Execute $targetExecutable `
    -Argument ('-config "{0}" -log "{1}"' -f $configPath, $logPath) `
    -WorkingDirectory $installRoot
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet `
    -StartWhenAvailable `
    -MultipleInstances IgnoreNew `
    -RestartCount 3 `
    -RestartInterval (New-TimeSpan -Minutes 1) `
    -ExecutionTimeLimit ([TimeSpan]::Zero)

Register-ScheduledTask `
    -TaskName $taskName `
    -Action $action `
    -Trigger $trigger `
    -Principal $principal `
    -Settings $settings `
    -Description 'Hearth local game server management panel' `
    -Force | Out-Null

Start-ScheduledTask -TaskName $taskName

if ($palworldReady) {
    $settingsText = [IO.File]::ReadAllText($settingsFile)
    if ($settingsText -notmatch 'RESTAPIEnabled\s*=\s*True') {
        Write-Warning 'Palworld RESTAPIEnabled is not True. Safe update and running backup remain locked; stop/restart require an explicit force-stop risk confirmation.'
    }
    if ($settingsText -match 'AdminPassword\s*=\s*""') {
        Write-Warning 'Palworld AdminPassword is empty. Set it before enabling safe management actions.'
    }
}
else {
    Write-Host 'No ready Palworld installation was found. Sign in as administrator to use the read-only discovery and installation wizard.'
}

$healthVerified = $false
$lastHealthError = $null
for ($attempt = 0; $attempt -lt 10; $attempt++) {
    Start-Sleep -Seconds 1
    try {
        $health = Invoke-RestMethod -Uri 'http://127.0.0.1:8080/api/v1/health' -TimeoutSec 2
        if ($health.status -eq 'ok' -and $health.version -eq $expectedVersion) {
            $healthVerified = $true
            break
        }
        $lastHealthError = "Expected panel version '$expectedVersion', received '$($health.version)'."
    }
    catch {
        $lastHealthError = $_.Exception.Message
    }
}
if (-not $healthVerified) {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    if ($null -ne $executableBackupPath -and (Test-Path -LiteralPath $executableBackupPath -PathType Leaf)) {
        Copy-Item -LiteralPath $executableBackupPath -Destination $targetExecutable -Force
        if ($null -ne $updaterBackupPath -and (Test-Path -LiteralPath $updaterBackupPath -PathType Leaf)) {
            Copy-Item -LiteralPath $updaterBackupPath -Destination $targetUpdater -Force
        }
        else {
            Remove-Item -LiteralPath $targetUpdater -Force -ErrorAction SilentlyContinue
        }
        Start-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    }
    else {
        Unregister-ScheduledTask -TaskName $taskName -Confirm:$false -ErrorAction SilentlyContinue
    }
    throw "The scheduled task was registered, but the panel health check failed: $lastHealthError"
}

Write-Host ''
Write-Host 'Hearth installed successfully.'
Write-Host "Hearth version: $expectedVersion"
Write-Host 'Open http://127.0.0.1:8080 in a browser inside the Windows RDP session.'
Write-Host "Hearth log: $logPath"
Write-Host 'The running Palworld process was not changed.'
