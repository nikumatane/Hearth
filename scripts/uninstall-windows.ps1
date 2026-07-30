<#
.SYNOPSIS
Unregisters the panel startup task without touching Palworld or panel data.
#>

[CmdletBinding()]
param()

#Requires -RunAsAdministrator

Set-StrictMode -Version 2.0
$ErrorActionPreference = 'Stop'

$taskName = 'Hearth'
$task = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($null -ne $task) {
    Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
    Write-Host "Removed scheduled task '$taskName'."
}
else {
    Write-Host 'Hearth was not installed.'
}

Write-Host 'Hearth files under ProgramData were preserved for recovery.'
Write-Host 'Palworld, SteamCMD, saves, configuration, and backups were not changed.'
