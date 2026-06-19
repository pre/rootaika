#Requires -Version 5.1
<#
.SYNOPSIS
  Stops the VirtualBox rootaika test client and its overlays.

.DESCRIPTION
  Stops rootaika.exe service/agent processes started by vbox-windows-watch.ps1
  and cleans up PowerShell overlay processes that can outlive the agent when a
  locked debug client is killed.
#>
[CmdletBinding()]
param(
    [switch]$Quiet
)

$ErrorActionPreference = "Stop"

function Write-StopMessage {
    param(
        [string]$Message,
        [System.ConsoleColor]$Color = [System.ConsoleColor]::Gray
    )
    if (-not $Quiet) {
        Write-Host $Message -ForegroundColor $Color
    }
}

function Get-ProcessTable {
    $table = @{}
    Get-CimInstance Win32_Process | ForEach-Object {
        $table[[int]$_.ProcessId] = $_
    }
    return $table
}

function Add-ChildProcessIds {
    param(
        [hashtable]$ProcessTable,
        [int]$ParentId,
        [System.Collections.Generic.HashSet[int]]$Result
    )

    foreach ($proc in $ProcessTable.Values) {
        if ([int]$proc.ParentProcessId -ne $ParentId) {
            continue
        }
        if ($Result.Add([int]$proc.ProcessId)) {
            Add-ChildProcessIds -ProcessTable $ProcessTable -ParentId ([int]$proc.ProcessId) -Result $Result
        }
    }
}

function Stop-ProcessIds {
    param(
        [int[]]$ProcessIds
    )

    $unique = $ProcessIds | Where-Object { $_ -gt 0 -and $_ -ne $PID } | Sort-Object -Unique
    foreach ($processId in $unique) {
        try {
            Stop-Process -Id $processId -Force -ErrorAction Stop
        } catch [Microsoft.PowerShell.Commands.ProcessCommandException] {
            # Already exited.
        } catch [System.ArgumentException] {
            # Already exited.
        }
    }
}

function Stop-RootaikaClient {
    $processTable = Get-ProcessTable
    $ids = [System.Collections.Generic.HashSet[int]]::new()

    foreach ($proc in $processTable.Values) {
        if ($proc.Name -eq "rootaika.exe") {
            [void]$ids.Add([int]$proc.ProcessId)
            Add-ChildProcessIds -ProcessTable $processTable -ParentId ([int]$proc.ProcessId) -Result $ids
        }
    }

    foreach ($proc in $processTable.Values) {
        if ($proc.Name -notin @("powershell.exe", "pwsh.exe")) {
            continue
        }
        $commandLine = [string]$proc.CommandLine
        if ($commandLine -match "ROOTAIKA_LOCK_MESSAGE|ROOTAIKA_WARN_SECONDS|ROOTAIKA_SOUND_PATH|RootaikaNative|RootaikaWarnNative") {
            [void]$ids.Add([int]$proc.ProcessId)
        }
    }

    if ($ids.Count -eq 0) {
        Write-StopMessage "No rootaika client processes are running." Cyan
        return
    }

    Write-StopMessage "Stopping rootaika client process(es): $($ids.Count)" Yellow
    Stop-ProcessIds -ProcessIds ([int[]]$ids)

    $deadline = (Get-Date).AddSeconds(5)
    while ((Get-Process rootaika -ErrorAction SilentlyContinue) -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 200
    }

    $leftovers = Get-Process rootaika -ErrorAction SilentlyContinue
    if ($leftovers) {
        throw "rootaika.exe did not exit"
    }

    Write-StopMessage "rootaika client stopped." Green
}

Stop-RootaikaClient
