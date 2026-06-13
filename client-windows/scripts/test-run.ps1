<#
.SYNOPSIS
  Runs a rootaika Windows client test session with a single command.

.DESCRIPTION
  Copies the binaries cross-compiled in WSL (client-windows/dist) into a local
  temp directory, sets the server address and credentials as environment
  variables, and starts rootaika-service. The service starts the agent via its
  watchdog from the same directory, so the agent reports the real foreground
  process (e.g. firefox.exe) from this Windows machine.

  Run as a NORMAL user (not as admin) so the agent sees the same session as
  your browser. Stop with Ctrl+C.

.NOTES
  The binaries must be cross-compiled in WSL first:
    cd client-windows
    GOOS=windows GOARCH=amd64 go build -o dist/rootaika-service.exe ./cmd/rootaika-service
    GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui" -o dist/rootaika-agent.exe ./cmd/rootaika-agent
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
$ServerUrl      = "http://192.168.68.126:8080"
$ClientUsername = "client"
$ClientPassword = "client"
# -------------------------------------

$serviceExe = Join-Path $DistDir "rootaika-service.exe"
$agentExe   = Join-Path $DistDir "rootaika-agent.exe"

if (-not (Test-Path $serviceExe) -or -not (Test-Path $agentExe)) {
    throw "Binaries not found in $DistDir. Cross-compile them in WSL first (see the script's .NOTES)."
}

# An exe cannot be reliably launched from a UNC path, so copy into a local directory.
New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null
Copy-Item $serviceExe, $agentExe $WorkDir -Force
Write-Host "Binaries copied -> $WorkDir" -ForegroundColor Green

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
Write-Host "Starting rootaika-service. Stop with Ctrl+C." -ForegroundColor Yellow
Write-Host "Keep Firefox in the foreground for a moment, wait ~a minute, then refresh the dashboard." -ForegroundColor Yellow
Write-Host ""

# Run the service from the same directory so its watchdog finds rootaika-agent.exe next to it.
Push-Location $WorkDir
try {
    & (Join-Path $WorkDir "rootaika-service.exe")
}
finally {
    Pop-Location
}
