#Requires -Version 5.1
#Requires -RunAsAdministrator
<#
.SYNOPSIS
  Installs the Finnish (fi-FI) text-to-speech voice used by the rootaika lock
  warning, then verifies it can speak.
.DESCRIPTION
  The lock warning speaks a Finnish countdown ("2 minuuttia jaljella ennen
  lukitusta"). Windows ships English voices only by default, so without a Finnish
  voice the warning falls back to whatever default voice exists, which mangles the
  Finnish pronunciation.

  Windows bundles the speech voice inside the Finnish language pack. This script
  installs the Finnish language with its optional speech feature via
  Install-Language (Windows 11 / Server 2022+) and reports the installed voices.

  On Windows 10, Install-Language is unavailable; the script prints manual steps
  (Settings -> Time & language -> Language & region -> add Finnish -> Language
  options -> install Speech) and still runs the verification step.
.EXAMPLE
  .\install-heidi-voice.ps1
.NOTES
  Run from an elevated PowerShell prompt. A reboot or sign-out may be required
  before the new voice appears to all processes.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"

function Get-FinnishVoices {
    Add-Type -AssemblyName System.Speech
    $synth = New-Object System.Speech.Synthesis.SpeechSynthesizer
    return $synth.GetInstalledVoices() |
        Where-Object { $_.Enabled -and $_.VoiceInfo.Culture.Name -eq 'fi-FI' } |
        ForEach-Object { $_.VoiceInfo.Name }
}

Write-Host "Checking for an existing Finnish (fi-FI) speech voice..."
$existing = Get-FinnishVoices
if ($existing) {
    Write-Host "Finnish voice already installed: $($existing -join ', ')"
} elseif (Get-Command Install-Language -ErrorAction SilentlyContinue) {
    Write-Host "Installing the Finnish language pack (includes the speech voice)..."
    Install-Language -Language fi-FI
    Write-Host "Install requested. A sign-out or reboot may be needed for it to register."
} else {
    Write-Warning "Install-Language is not available on this Windows version (likely Windows 10)."
    Write-Host "Install the Finnish speech voice manually:"
    Write-Host "  1. Settings -> Time & language -> Language & region"
    Write-Host "  2. Add a language -> Finnish (suomi)"
    Write-Host "  3. Open its Language options -> Speech -> Download"
    Write-Host "  4. Sign out and back in, then re-run this script to verify."
}

Write-Host ""
Write-Host "Verifying speech output..."
try {
    Add-Type -AssemblyName System.Speech
    $synth = New-Object System.Speech.Synthesis.SpeechSynthesizer
    $fi = $synth.GetInstalledVoices() |
          Where-Object { $_.Enabled -and $_.VoiceInfo.Culture.Name -eq 'fi-FI' } |
          Select-Object -First 1
    if ($fi) {
        $synth.SelectVoice($fi.VoiceInfo.Name)
        Write-Host "Speaking test phrase with voice: $($fi.VoiceInfo.Name)"
    } else {
        Write-Warning "No Finnish voice active yet; speaking with the default voice (may need a reboot)."
    }
    $synth.Speak("Testi. Kaksi minuuttia jaljella ennen lukitusta.")
    Write-Host "Done."
} catch {
    Write-Warning "Speech verification failed: $($_.Exception.Message)"
}
