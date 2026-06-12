#Requires -Version 5.1
<#
.SYNOPSIS
  Builds the rootaika Windows client binaries.
.DESCRIPTION
  Produces rootaika-service.exe and rootaika-agent.exe into the output directory.
  The agent is linked with -H=windowsgui so it has no console window by default;
  debug mode allocates a console on demand at runtime.
#>
[CmdletBinding()]
param(
    [string]$OutDir = (Join-Path $PSScriptRoot "..\dist")
)

$ErrorActionPreference = "Stop"
$clientRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$OutDir = Resolve-Path $OutDir

Push-Location $clientRoot
try {
    Write-Host "Building rootaika-service.exe..."
    & go build -o (Join-Path $OutDir "rootaika-service.exe") ./cmd/rootaika-service
    if ($LASTEXITCODE -ne 0) { throw "service build failed" }

    Write-Host "Building rootaika-agent.exe (no console)..."
    & go build -ldflags "-H=windowsgui" -o (Join-Path $OutDir "rootaika-agent.exe") ./cmd/rootaika-agent
    if ($LASTEXITCODE -ne 0) { throw "agent build failed" }

    Write-Host "Built binaries into $OutDir"
}
finally {
    Pop-Location
}
