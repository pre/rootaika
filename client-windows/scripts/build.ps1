#Requires -Version 5.1
<#
.SYNOPSIS
  Builds the rootaika Windows client binary.
.DESCRIPTION
  Produces a single rootaika.exe into the output directory. One exe runs both the
  service and the user-session agent, dispatched on the first argument
  (rootaika.exe service / rootaika.exe agent / rootaika.exe apply-update). It is
  linked with -H=windowsgui so neither process shows a console by default; debug
  mode allocates one on demand at runtime.
.PARAMETER Version
  Version string baked into the binary via ldflags. Defaults to "dev".
#>
[CmdletBinding()]
param(
    [string]$OutDir = (Join-Path $PSScriptRoot "..\dist"),
    [string]$Version = "dev"
)

$ErrorActionPreference = "Stop"
$clientRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$OutDir = Resolve-Path $OutDir

Push-Location $clientRoot
try {
    Write-Host "Building rootaika.exe (version $Version, no console)..."
    $ldflags = "-H=windowsgui -X rootaika/client-windows/internal/version.Version=$Version"
    & go build -ldflags $ldflags -o (Join-Path $OutDir "rootaika.exe") ./cmd/rootaika
    if ($LASTEXITCODE -ne 0) { throw "build failed" }

    Write-Host "Built rootaika.exe into $OutDir"
}
finally {
    Pop-Location
}
