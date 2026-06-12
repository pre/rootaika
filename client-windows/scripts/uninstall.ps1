#Requires -Version 5.1
#Requires -RunAsAdministrator
<#
.SYNOPSIS
  Uninstalls the rootaika Windows client.
.DESCRIPTION
  Stops and deletes the service, removes the agent autostart entry, stops any
  running agent, and optionally removes installed files and config.
.PARAMETER Purge
  Also delete the Program Files install dir and the ProgramData config/buffer.
#>
[CmdletBinding()]
param(
    [switch]$Purge
)

$ErrorActionPreference = "Stop"

$serviceName = "rootaika-service"
$installDir  = Join-Path $env:ProgramFiles "rootaika"
$dataDir     = Join-Path $env:ProgramData "rootaika"
$runKey      = "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run"
$runValue    = "rootaika-agent"

$existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "Stopping and deleting service $serviceName..."
    & sc.exe stop $serviceName | Out-Null
    Start-Sleep -Seconds 2
    & sc.exe delete $serviceName | Out-Null
} else {
    Write-Host "Service $serviceName not found, skipping."
}

if (Get-ItemProperty -Path $runKey -Name $runValue -ErrorAction SilentlyContinue) {
    Write-Host "Removing agent autostart entry..."
    Remove-ItemProperty -Path $runKey -Name $runValue
}

$agent = Get-Process -Name "rootaika-agent" -ErrorAction SilentlyContinue
if ($agent) {
    Write-Host "Stopping running agent process(es)..."
    $agent | Stop-Process -Force
}

if ($Purge) {
    if (Test-Path $installDir) {
        Write-Host "Removing $installDir..."
        Remove-Item -Recurse -Force $installDir
    }
    if (Test-Path $dataDir) {
        Write-Host "Removing $dataDir..."
        Remove-Item -Recurse -Force $dataDir
    }
}

Write-Host "rootaika client uninstalled."
