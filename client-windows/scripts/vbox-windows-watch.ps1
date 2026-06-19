#Requires -Version 5.1
<#
.SYNOPSIS
  Watches a shared-folder launch request and restarts the rootaika test client.
.DESCRIPTION
  Pair this with vbox-macos-launch.sh. The macOS side builds dist\rootaika.exe
  and writes .vbox-launch\request.json. This watcher runs inside Windows,
  copies the new exe to a local temp directory, stops the old rootaika process,
  and starts "rootaika.exe service" so the watchdog starts the agent too.
#>
[CmdletBinding()]
param(
    [string]$RequestPath = "",
    [string]$WorkDir = "",
    [int]$PollSeconds = 1,
    [switch]$Once,
    [switch]$InstallAutostart,
    [switch]$UninstallAutostart
)

$ErrorActionPreference = "Stop"

$ScriptPath = if ($PSCommandPath) { $PSCommandPath } else { $MyInvocation.MyCommand.Path }
if (-not $ScriptPath) {
    throw "could not resolve script path"
}
$ScriptDir = Split-Path -Parent $ScriptPath
if (-not $RequestPath) {
    $RequestPath = Join-Path $ScriptDir "..\.vbox-launch\request.json"
}
if (-not $WorkDir) {
    $WorkDir = Join-Path $env:TEMP "rootaika-vbox"
}

$RequestPath = [System.IO.Path]::GetFullPath($RequestPath)
$WorkDir = [System.IO.Path]::GetFullPath($WorkDir)

$runKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
$runValue = "rootaika-vbox-watch"

function Quote-CommandArg {
    param([string]$Value)
    return '"' + ($Value -replace '"', '\"') + '"'
}

function Write-Status {
    param(
        [string]$RequestId,
        [string]$Status,
        [string]$Message = "",
        [int]$ProcessId = 0
    )
    $stateDir = Split-Path -Parent $RequestPath
    New-Item -ItemType Directory -Force -Path $stateDir | Out-Null
    $state = [pscustomobject]@{
        request_id = $RequestId
        status = $Status
        message = $Message
        process_id = $ProcessId
        work_dir = $WorkDir
        updated_at_utc = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    }
    $state | ConvertTo-Json -Depth 5 | Set-Content -Path (Join-Path $stateDir "windows-watcher-state.json") -Encoding UTF8
}

function Get-LastRequestId {
    $statePath = Join-Path (Split-Path -Parent $RequestPath) "windows-watcher-state.json"
    if (-not (Test-Path $statePath)) {
        return ""
    }
    try {
        $state = Get-Content $statePath -Raw | ConvertFrom-Json
        return [string]$state.request_id
    } catch {
        return ""
    }
}

function Stop-RootaikaProcesses {
    $stopScript = Join-Path $ScriptDir "vbox-windows-stop.ps1"
    if (-not (Test-Path $stopScript)) {
        throw "stop helper not found at $stopScript"
    }
    & $stopScript -Quiet
}

function Copy-ExeWithRetry {
    param(
        [string]$Source,
        [string]$Destination
    )
    for ($attempt = 1; $attempt -le 5; $attempt++) {
        try {
            Copy-Item $Source $Destination -Force
            return
        } catch {
            if ($attempt -eq 5) {
                throw
            }
            Start-Sleep -Milliseconds 300
        }
    }
}

function Start-RootaikaClient {
    param([pscustomobject]$Request)

    $clientRoot = Resolve-Path (Join-Path $ScriptDir "..")
    $sourceExe = Join-Path $clientRoot "dist\rootaika.exe"
    if (-not (Test-Path $sourceExe)) {
        throw "rootaika.exe not found at $sourceExe. Run vbox-macos-launch.sh on macOS first."
    }

    if ($Request.sha256) {
        $actualHash = (Get-FileHash -Algorithm SHA256 -Path $sourceExe).Hash.ToLowerInvariant()
        $wantedHash = ([string]$Request.sha256).Trim().ToLowerInvariant()
        if ($actualHash -ne $wantedHash) {
            throw "rootaika.exe sha256 mismatch: got $actualHash want $wantedHash"
        }
    }

    Stop-RootaikaProcesses

    New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null
    $localExe = Join-Path $WorkDir "rootaika.exe"
    Copy-ExeWithRetry -Source $sourceExe -Destination $localExe
    Unblock-File $localExe -ErrorAction SilentlyContinue

    $env:ROOTAIKA_HOME = $WorkDir
    if ($Request.server_url) { $env:ROOTAIKA_SERVER_URL = [string]$Request.server_url }
    if ($Request.client_username) { $env:ROOTAIKA_CLIENT_USERNAME = [string]$Request.client_username }
    if ($Request.client_password) { $env:ROOTAIKA_CLIENT_PASSWORD = [string]$Request.client_password }

    Write-Host "Starting rootaika client from $localExe" -ForegroundColor Green
    Write-Host "Server: $env:ROOTAIKA_SERVER_URL" -ForegroundColor Cyan
    $process = Start-Process -FilePath $localExe -ArgumentList "service" -WorkingDirectory $WorkDir -WindowStyle Hidden -PassThru
    Write-Status -RequestId $Request.request_id -Status "started" -Message "rootaika.exe service started" -ProcessId $process.Id
}

function Install-Autostart {
    $command = "powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File $(Quote-CommandArg $ScriptPath) -RequestPath $(Quote-CommandArg $RequestPath) -WorkDir $(Quote-CommandArg $WorkDir) -PollSeconds $PollSeconds"
    New-Item -Path $runKey -Force | Out-Null
    Set-ItemProperty -Path $runKey -Name $runValue -Value $command
    Write-Host "Installed watcher autostart for the current Windows user." -ForegroundColor Green
}

function Uninstall-Autostart {
    Remove-ItemProperty -Path $runKey -Name $runValue -ErrorAction SilentlyContinue
    Write-Host "Removed watcher autostart for the current Windows user." -ForegroundColor Green
}

if ($InstallAutostart) {
    Install-Autostart
}
if ($UninstallAutostart) {
    Uninstall-Autostart
    if (-not $InstallAutostart) {
        exit 0
    }
}

$lastRequestId = Get-LastRequestId
Write-Host "Watching $RequestPath" -ForegroundColor Cyan
Write-Host "Local client work dir: $WorkDir" -ForegroundColor Cyan

while ($true) {
    if (Test-Path $RequestPath) {
        try {
            $request = Get-Content $RequestPath -Raw | ConvertFrom-Json
            if ($request.request_id -and $request.request_id -ne $lastRequestId) {
                Write-Host "Launch request $($request.request_id)" -ForegroundColor Yellow
                Start-RootaikaClient -Request $request
                $lastRequestId = $request.request_id
            }
        } catch {
            Write-Status -RequestId $lastRequestId -Status "error" -Message $_.Exception.Message
            Write-Host $_.Exception.Message -ForegroundColor Red
        }
    }

    if ($Once) {
        break
    }
    Start-Sleep -Seconds $PollSeconds
}
