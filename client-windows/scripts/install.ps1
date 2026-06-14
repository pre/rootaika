#Requires -Version 5.1
#Requires -RunAsAdministrator
<#
.SYNOPSIS
  Installs the rootaika Windows client (service + user-session agent).
.DESCRIPTION
  Copies the single rootaika.exe to Program Files, writes the client config under
  ProgramData, registers rootaika-service as an auto-start Windows service with
  crash recovery, registers the agent to launch at user logon, and starts the
  service. Both processes run from the one exe (service / agent subcommands), so a
  later OTA update only needs to swap that file. Run from an elevated PowerShell
  prompt.
.PARAMETER ServerUrl
  Base URL of the rootaika server, e.g. http://192.168.1.10:8080
.PARAMETER ClientPassword
  Shared client Basic Auth password configured on the server.
.PARAMETER ClientUsername
  Shared client Basic Auth username. Defaults to "client".
.PARAMETER SourceDir
  Directory containing rootaika.exe. Defaults to ..\dist relative to this script
  (see build.ps1).
.EXAMPLE
  .\install.ps1 -ServerUrl http://192.168.1.10:8080 -ClientPassword s3cret
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$ServerUrl,
    [Parameter(Mandatory = $true)][string]$ClientPassword,
    [string]$ClientUsername = "client",
    [string]$SourceDir = (Join-Path $PSScriptRoot "..\dist")
)

$ErrorActionPreference = "Stop"

$serviceName = "rootaika-service"
$installDir  = Join-Path $env:ProgramFiles "rootaika"
$dataDir     = Join-Path $env:ProgramData "rootaika"
$configPath  = Join-Path $dataDir "client.json"
$exePath     = Join-Path $installDir "rootaika.exe"
$runKey      = "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Run"
$runValue    = "rootaika-agent"

$srcExe = Join-Path $SourceDir "rootaika.exe"
if (-not (Test-Path $srcExe)) {
    throw "rootaika.exe not found in $SourceDir. Run build.ps1 first."
}

Write-Host "Installing binary to $installDir..."
New-Item -ItemType Directory -Force -Path $installDir | Out-Null
New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
Copy-Item $srcExe $exePath -Force

Write-Host "Writing config to $configPath..."
if (Test-Path $configPath) {
    $config = Get-Content $configPath -Raw | ConvertFrom-Json
} else {
    $config = [pscustomobject]@{}
}
$config | Add-Member -NotePropertyName server_url -NotePropertyValue $ServerUrl -Force
$config | Add-Member -NotePropertyName client_username -NotePropertyValue $ClientUsername -Force
$config | Add-Member -NotePropertyName client_password -NotePropertyValue $ClientPassword -Force
$config | ConvertTo-Json -Depth 10 | Set-Content -Path $configPath -Encoding UTF8

# Remove any prior service so binPath/config changes take effect cleanly.
$existing = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($existing) {
    Write-Host "Existing service found, removing it first..."
    & sc.exe stop $serviceName | Out-Null
    Start-Sleep -Seconds 2
    & sc.exe delete $serviceName | Out-Null
    Start-Sleep -Seconds 1
}

$binPath = "`"$exePath`" service -config `"$configPath`""
Write-Host "Registering service $serviceName..."
& sc.exe create $serviceName binPath= $binPath start= auto DisplayName= "rootaika client service" | Out-Null
if ($LASTEXITCODE -ne 0) { throw "sc.exe create failed ($LASTEXITCODE)" }

# Restart on crash: after 5s, three times, reset the counter after 60s.
& sc.exe failure $serviceName reset= 60 actions= restart/5000/restart/5000/restart/5000 | Out-Null
& sc.exe description $serviceName "rootaika screen-time client. Buffers activity events and reports to the rootaika server." | Out-Null

Write-Host "Registering agent autostart at user logon..."
Set-ItemProperty -Path $runKey -Name $runValue -Value "`"$exePath`" agent -config `"$configPath`""

Write-Host "Starting service..."
& sc.exe start $serviceName | Out-Null

# The agent runs in the user session. Launch it now for the current user so it
# starts without requiring a logoff/logon cycle.
Write-Host "Launching agent for the current session..."
Start-Process -FilePath $exePath -ArgumentList "agent", "-config", $configPath

Write-Host ""
Write-Host "rootaika client installed."
Write-Host "  Service : $serviceName (auto-start, restart on crash)"
Write-Host "  Agent   : autostarts at logon via HKLM Run, started now too"
Write-Host "  Binary  : $exePath (service / agent subcommands)"
Write-Host "  Config  : $configPath"
Write-Host "  Server  : $ServerUrl"
