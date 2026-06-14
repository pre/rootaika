<#
.SYNOPSIS
  Runs a rootaika Windows client test session with a single command.

.DESCRIPTION
  Copies the combined rootaika.exe cross-compiled in WSL (client-windows/dist)
  into a local temp directory, sets the server address and credentials as
  environment variables, and starts it with the service subcommand. The service
  starts the agent via its watchdog from the same exe, so the agent reports the
  real foreground process (e.g. firefox.exe) from this Windows machine.

  Run as a NORMAL user (not as admin) so the agent sees the same session as
  your browser. Stop with Ctrl+C.

.NOTES
  The binary must be cross-compiled in WSL first:
    cd client-windows
    GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui" -o dist/rootaika.exe ./cmd/rootaika
#>

[CmdletBinding()]
param(
    # UNC path to the WSL dist directory that holds the .exe files.
    [string]$DistDir = "\\wsl.localhost\Ubuntu-24.04\home\prepo\dev\oma\rootaika\client-windows\dist",

    # Local working directory. Same directory every run -> same client_id (same device on the server).
    [string]$WorkDir = (Join-Path $env:TEMP "rootaika-test")
)

$ErrorActionPreference = "Stop"

# --- Hard-coded test configuration ---
$ServerUrl      = "http://192.168.68.111:8080"
$ClientUsername = "client"
$ClientPassword = "client"
# -------------------------------------

$clientExe = Join-Path $DistDir "rootaika.exe"

if (-not (Test-Path $clientExe)) {
    throw "rootaika.exe not found in $DistDir. Cross-compile it in WSL first (see the script's .NOTES)."
}

# Stop any leftover processes from a previous run. A service that was not shut
# down cleanly keeps holding the agent endpoint port (default 127.0.0.1:48611)
# and locks the exe file, which would otherwise fail the copy and the bind.
$leftovers = Get-Process rootaika -ErrorAction SilentlyContinue
if ($leftovers) {
    Write-Host "Stopping leftover rootaika processes from a previous run..." -ForegroundColor Yellow
    $leftovers | Stop-Process -Force
    Start-Sleep -Milliseconds 500
}

# An exe cannot be reliably launched from a UNC path, so copy into a local directory.
New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null
Copy-Item $clientExe $WorkDir -Force
# Copying from the \\wsl.localhost\ UNC share stamps a network-zone Mark-of-the-Web;
# clear it so that is one less thing in the way.
Unblock-File (Join-Path $WorkDir "rootaika.exe")
Write-Host "Binary copied -> $WorkDir" -ForegroundColor Green

# Environment variables: ROOTAIKA_HOME points client.json + the local SQLite buffer
# to this directory, the rest point to the server and credentials.
$env:ROOTAIKA_HOME            = $WorkDir
$env:ROOTAIKA_SERVER_URL      = $ServerUrl
$env:ROOTAIKA_CLIENT_USERNAME = $ClientUsername
$env:ROOTAIKA_CLIENT_PASSWORD = $ClientPassword

Write-Host "Server: $ServerUrl" -ForegroundColor Cyan
Write-Host "User:   $ClientUsername" -ForegroundColor Cyan
Write-Host "Dir:    $WorkDir" -ForegroundColor Cyan
Write-Host ""
Write-Host "Starting rootaika.exe service. Stop with Ctrl+C." -ForegroundColor Yellow
Write-Host "Keep Firefox in the foreground for a moment, wait ~a minute, then refresh the dashboard." -ForegroundColor Yellow
Write-Host ""

$localExe = Join-Path $WorkDir "rootaika.exe"

# Run the service from the same directory so its watchdog launches the agent from
# the same rootaika.exe (os.Executable()).
Push-Location $WorkDir
try {
    & $localExe service
}
finally {
    Pop-Location
}
