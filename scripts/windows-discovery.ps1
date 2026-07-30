<#
.SYNOPSIS
Collects a read-only, privacy-conscious snapshot of a Windows game server.

.DESCRIPTION
The script does not install software, stop processes, change services, read
configuration contents, inspect credentials, or make network requests.

It writes one JSON report containing:
- Windows, PowerShell, CPU, memory, and fixed-disk information
- Palworld, Don't Starve Together, and SteamCMD process metadata
- Related Windows service and scheduled-task names
- Relevant listening ports
- Paths and metadata for known game-server files under a bounded set of roots

.PARAMETER OutputPath
Destination JSON file. Defaults to the current user's Desktop.

.PARAMETER SearchRoot
Optional game installation roots to scan in addition to common locations and
directories inferred from running game processes.

.EXAMPLE
Set-ExecutionPolicy -Scope Process Bypass
.\windows-discovery.ps1

.EXAMPLE
.\windows-discovery.ps1 -SearchRoot 'D:\SteamCMD', 'E:\GameServers'
#>

[CmdletBinding()]
param(
    [Parameter()]
    [string]$OutputPath,

    [Parameter()]
    [string[]]$SearchRoot = @()
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$script:Warnings = New-Object System.Collections.Generic.List[string]

function Invoke-Safe {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,

        [Parameter(Mandatory = $true)]
        [scriptblock]$Operation,

        [Parameter()]
        $Fallback = $null
    )

    try {
        return & $Operation
    }
    catch {
        $safeMessage = Protect-Text $_.Exception.Message
        $script:Warnings.Add(('{0}: {1}' -f $Name, $safeMessage))
        return $Fallback
    }
}

function Protect-Text {
    param(
        [Parameter()]
        [AllowNull()]
        [string]$Value
    )

    if ([string]::IsNullOrWhiteSpace($Value)) {
        return $Value
    }

    $protected = $Value
    if (-not [string]::IsNullOrWhiteSpace($env:USERPROFILE)) {
        $protected = $protected -replace [regex]::Escape($env:USERPROFILE), '%USERPROFILE%'
    }
    if (-not [string]::IsNullOrWhiteSpace($env:USERNAME)) {
        $protected = $protected -replace [regex]::Escape($env:USERNAME), '<USER>'
    }
    if (-not [string]::IsNullOrWhiteSpace($env:COMPUTERNAME)) {
        $protected = $protected -replace [regex]::Escape($env:COMPUTERNAME), '<COMPUTER>'
    }
    return $protected
}

function Convert-ToRoundedGB {
    param(
        [Parameter()]
        [AllowNull()]
        $Bytes
    )

    if ($null -eq $Bytes) {
        return $null
    }
    return [math]::Round(([double]$Bytes / 1GB), 2)
}

function Get-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Get-FileSnapshot {
    param(
        [Parameter(Mandatory = $true)]
        [System.IO.FileInfo]$File
    )

    return [pscustomobject]@{
        name          = $File.Name
        path          = Protect-Text $File.FullName
        sizeBytes     = $File.Length
        lastWriteTime = $File.LastWriteTimeUtc.ToString('o')
    }
}

function Find-GameServerFiles {
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [string[]]$Roots,

        [Parameter()]
        [int]$MaxDepth = 4,

        [Parameter()]
        [int]$MaxDirectories = 1500,

        [Parameter()]
        [int]$MaxResults = 150
    )

    $targetPattern = '^(steamcmd\.exe|PalServer\.exe|PalServer-Win64-Shipping-Cmd\.exe|dontstarve_dedicated_server_nullrenderer(?:_x64)?\.exe|PalWorldSettings\.ini|cluster\.ini|server\.ini|dedicated_server_mods_setup\.lua|(?:start|run)[^\\]*\.bat|appmanifest_(?:2394010|343050)\.acf)$'
    $queue = New-Object System.Collections.Queue
    $visited = @{}
    $results = New-Object System.Collections.Generic.List[object]
    $directoryCount = 0

    foreach ($root in $Roots) {
        if ([string]::IsNullOrWhiteSpace($root)) {
            continue
        }
        $resolved = Invoke-Safe -Name ('Resolve search root {0}' -f (Protect-Text $root)) -Operation {
            (Resolve-Path -LiteralPath $root).Path
        }
        if ($null -ne $resolved -and -not $visited.ContainsKey($resolved.ToLowerInvariant())) {
            $queue.Enqueue([pscustomobject]@{ Path = $resolved; Depth = 0 })
            $visited[$resolved.ToLowerInvariant()] = $true
        }
    }

    while ($queue.Count -gt 0 -and $directoryCount -lt $MaxDirectories -and $results.Count -lt $MaxResults) {
        $entry = $queue.Dequeue()
        $directoryCount++

        $children = Invoke-Safe -Name ('Inspect directory {0}' -f (Protect-Text $entry.Path)) -Operation {
            @(Get-ChildItem -LiteralPath $entry.Path -Force -ErrorAction Stop)
        } -Fallback @()

        foreach ($child in $children) {
            if ($results.Count -ge $MaxResults) {
                break
            }

            if ($child.PSIsContainer) {
                $isReparsePoint = (($child.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0)
                if (-not $isReparsePoint -and $entry.Depth -lt $MaxDepth) {
                    $key = $child.FullName.ToLowerInvariant()
                    if (-not $visited.ContainsKey($key)) {
                        $visited[$key] = $true
                        $queue.Enqueue([pscustomobject]@{
                            Path  = $child.FullName
                            Depth = $entry.Depth + 1
                        })
                    }
                }
                continue
            }

            if ($child.Name -match $targetPattern) {
                $results.Add((Get-FileSnapshot -File $child))
            }
        }
    }

    if ($directoryCount -ge $MaxDirectories) {
        $script:Warnings.Add('File discovery reached its directory safety limit; provide a narrower -SearchRoot if files are missing.')
    }
    if ($results.Count -ge $MaxResults) {
        $script:Warnings.Add('File discovery reached its result safety limit; provide a narrower -SearchRoot if needed.')
    }

    return @($results | Sort-Object path -Unique)
}

if ([string]::IsNullOrWhiteSpace($OutputPath)) {
    $desktop = [Environment]::GetFolderPath('Desktop')
    if ([string]::IsNullOrWhiteSpace($desktop)) {
        $desktop = (Get-Location).Path
    }
    $OutputPath = Join-Path $desktop ('hearth-server-discovery-{0}.json' -f (Get-Date -Format 'yyyyMMdd-HHmmss'))
}

$operatingSystem = Invoke-Safe -Name 'Operating system' -Operation {
    Get-CimInstance -ClassName Win32_OperatingSystem
}
$computerSystem = Invoke-Safe -Name 'Computer system' -Operation {
    Get-CimInstance -ClassName Win32_ComputerSystem
}
$processors = Invoke-Safe -Name 'Processor information' -Operation {
    @(Get-CimInstance -ClassName Win32_Processor)
} -Fallback @()

$fixedDisks = Invoke-Safe -Name 'Fixed disks' -Operation {
    @(
        Get-CimInstance -ClassName Win32_LogicalDisk -Filter 'DriveType = 3' |
            ForEach-Object {
                [pscustomobject]@{
                    drive        = $_.DeviceID
                    sizeGB       = Convert-ToRoundedGB $_.Size
                    freeGB       = Convert-ToRoundedGB $_.FreeSpace
                    fileSystem    = $_.FileSystem
                    volumeName    = Protect-Text $_.VolumeName
                }
            }
    )
} -Fallback @()

$processRootPaths = New-Object System.Collections.Generic.List[string]
$processCandidates = Invoke-Safe -Name 'Game processes' -Operation {
    @(
        Get-CimInstance -ClassName Win32_Process |
            Where-Object {
                $_.Name -match '^(PalServer|PalServer-Win64-Shipping-Cmd|dontstarve_dedicated_server_nullrenderer(?:_x64)?|steamcmd)\.exe$'
            } |
            ForEach-Object {
                $cimProcess = $_
                if (-not [string]::IsNullOrWhiteSpace($cimProcess.ExecutablePath)) {
                    $processDirectory = Split-Path -Parent $cimProcess.ExecutablePath
                    if (-not [string]::IsNullOrWhiteSpace($processDirectory)) {
                        $processRootPaths.Add($processDirectory)
                    }
                }
                $runtimeProcess = Get-Process -Id $cimProcess.ProcessId -ErrorAction SilentlyContinue
                $startedAt = $null
                $workingSetMB = $null
                $cpuSeconds = $null
                if ($null -ne $runtimeProcess) {
                    $workingSetMB = [math]::Round(($runtimeProcess.WorkingSet64 / 1MB), 2)
                    $cpuSeconds = [math]::Round($runtimeProcess.CPU, 2)
                    try {
                        $startedAt = $runtimeProcess.StartTime.ToUniversalTime().ToString('o')
                    }
                    catch {
                        $startedAt = $null
                    }
                }
                [pscustomobject]@{
                    name          = $cimProcess.Name
                    processId     = $cimProcess.ProcessId
                    parentId      = $cimProcess.ParentProcessId
                    executablePath = Protect-Text $cimProcess.ExecutablePath
                    workingSetMB  = $workingSetMB
                    cpuSeconds    = $cpuSeconds
                    startedAt     = $startedAt
                }
            }
    )
} -Fallback @()

$relatedServices = Invoke-Safe -Name 'Related services' -Operation {
    @(
        Get-CimInstance -ClassName Win32_Service |
            Where-Object {
                $_.Name -match '(?i)(pal|starve|dst|steam|nssm|winsw)' -or
                $_.DisplayName -match '(?i)(pal|starve|don.t starve|steam|nssm|winsw)'
            } |
            ForEach-Object {
                [pscustomobject]@{
                    name        = $_.Name
                    displayName = $_.DisplayName
                    state       = $_.State
                    startMode   = $_.StartMode
                    startName   = Protect-Text $_.StartName
                }
            }
    )
} -Fallback @()

$relatedTasks = Invoke-Safe -Name 'Related scheduled tasks' -Operation {
    @(
        Get-ScheduledTask |
            Where-Object {
                $_.TaskName -match '(?i)(pal|starve|dst|steam|game)'
            } |
            ForEach-Object {
                [pscustomobject]@{
                    taskName = $_.TaskName
                    taskPath = Protect-Text $_.TaskPath
                    state    = [string]$_.State
                }
            }
    )
} -Fallback @()

$interestingPorts = @(8211, 8212, 25575, 10999, 11000, 11001, 11002, 11003, 11004, 11005)
$udpEndpoints = Invoke-Safe -Name 'UDP endpoints' -Operation {
    @(
        Get-NetUDPEndpoint |
            Where-Object { $interestingPorts -contains $_.LocalPort } |
            ForEach-Object {
                [pscustomobject]@{
                    protocol      = 'UDP'
                    localAddress  = $_.LocalAddress
                    localPort     = $_.LocalPort
                    owningProcess = $_.OwningProcess
                }
            }
    )
} -Fallback @()

$tcpEndpoints = Invoke-Safe -Name 'TCP endpoints' -Operation {
    @(
        Get-NetTCPConnection -State Listen |
            Where-Object { $interestingPorts -contains $_.LocalPort } |
            ForEach-Object {
                [pscustomobject]@{
                    protocol      = 'TCP'
                    localAddress  = $_.LocalAddress
                    localPort     = $_.LocalPort
                    owningProcess = $_.OwningProcess
                }
            }
    )
} -Fallback @()

$commandLocations = Invoke-Safe -Name 'Management executable locations' -Operation {
    @(
        foreach ($commandName in @('steamcmd.exe', 'nssm.exe', 'winsw.exe')) {
            $commands = @(Get-Command $commandName -CommandType Application -ErrorAction SilentlyContinue)
            foreach ($command in $commands) {
                [pscustomobject]@{
                    name = $commandName
                    path = Protect-Text $command.Source
                }
            }
        }
    )
} -Fallback @()

$candidateRoots = New-Object System.Collections.Generic.List[string]
foreach ($path in @(
    'C:\steamcmd',
    'C:\SteamCMD',
    'C:\Games',
    'C:\GameServers',
    'D:\steamcmd',
    'D:\SteamCMD',
    'D:\Games',
    'D:\GameServers',
    'E:\steamcmd',
    'E:\SteamCMD',
    'E:\Games',
    'E:\GameServers'
)) {
    if (Test-Path -LiteralPath $path -PathType Container) {
        $candidateRoots.Add($path)
    }
}
foreach ($path in $SearchRoot) {
    if (-not [string]::IsNullOrWhiteSpace($path) -and (Test-Path -LiteralPath $path -PathType Container)) {
        $candidateRoots.Add($path)
    }
}
foreach ($path in $processRootPaths) {
    if (-not [string]::IsNullOrWhiteSpace($path) -and (Test-Path -LiteralPath $path -PathType Container)) {
        $candidateRoots.Add($path)
    }
}

$uniqueRoots = @($candidateRoots | Sort-Object -Unique)
$discoveredFiles = Find-GameServerFiles -Roots $uniqueRoots

$report = [ordered]@{
    schemaVersion = 1
    generatedAt   = (Get-Date).ToUniversalTime().ToString('o')
    collection    = [ordered]@{
        readOnly             = $true
        configurationContent = $false
        commandLines         = $false
        environmentVariables = $false
        networkRequests      = $false
        searchMaxDepth       = 4
    }
    windows       = [ordered]@{
        caption             = if ($null -ne $operatingSystem) { $operatingSystem.Caption } else { $null }
        version             = if ($null -ne $operatingSystem) { $operatingSystem.Version } else { $null }
        buildNumber         = if ($null -ne $operatingSystem) { $operatingSystem.BuildNumber } else { $null }
        architecture        = if ($null -ne $operatingSystem) { $operatingSystem.OSArchitecture } else { $null }
        powerShellVersion   = $PSVersionTable.PSVersion.ToString()
        isAdministrator     = (Invoke-Safe -Name 'Administrator check' -Operation { Get-IsAdministrator } -Fallback $false)
        logicalProcessors   = if ($null -ne $computerSystem) { $computerSystem.NumberOfLogicalProcessors } else { $null }
        totalMemoryGB       = if ($null -ne $computerSystem) { Convert-ToRoundedGB $computerSystem.TotalPhysicalMemory } else { $null }
        processors          = @(
            $processors | ForEach-Object {
                [pscustomobject]@{
                    name              = $_.Name
                    cores             = $_.NumberOfCores
                    logicalProcessors = $_.NumberOfLogicalProcessors
                }
            }
        )
    }
    disks         = $fixedDisks
    processes     = $processCandidates
    services      = $relatedServices
    scheduledTasks = $relatedTasks
    listeningPorts = @($udpEndpoints) + @($tcpEndpoints)
    commands      = $commandLocations
    searchRoots   = @($uniqueRoots | ForEach-Object { Protect-Text $_ })
    discoveredFiles = $discoveredFiles
    warnings      = @($script:Warnings)
}

$outputDirectory = Split-Path -Parent $OutputPath
if (-not [string]::IsNullOrWhiteSpace($outputDirectory) -and -not (Test-Path -LiteralPath $outputDirectory)) {
    throw ('Output directory does not exist: {0}' -f $outputDirectory)
}

$json = $report | ConvertTo-Json -Depth 8
$json | Set-Content -LiteralPath $OutputPath -Encoding UTF8

# Verify that the generated file is valid JSON before reporting success.
$null = Get-Content -LiteralPath $OutputPath -Raw | ConvertFrom-Json

Write-Host ''
Write-Host 'Windows game-server discovery completed.' -ForegroundColor Green
Write-Host ('Report: {0}' -f $OutputPath)
Write-Host ('Processes found: {0}' -f @($processCandidates).Count)
Write-Host ('Candidate files found: {0}' -f @($discoveredFiles).Count)
Write-Host ('Warnings: {0}' -f $script:Warnings.Count)
Write-Host 'Review the JSON before sharing it. It contains paths, but no file contents or command lines.'
