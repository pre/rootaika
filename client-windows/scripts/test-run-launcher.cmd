@echo off
REM Double-click launcher for the rootaika client test run.
REM Runs test-run.ps1 (located next to this file) with the execution policy bypassed.
powershell -ExecutionPolicy Bypass -File "%~dp0test-run.ps1"
pause
