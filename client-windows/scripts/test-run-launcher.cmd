@echo off
REM Double-click launcher for the rootaika client test run.
REM Cross-compiles the binaries in WSL, then runs test-run.ps1 (next to this file).

set WSL_DISTRO=Ubuntu-24.04
set REPO_DIR=/home/prepo/dev/oma/rootaika/client-windows
set GO=/home/linuxbrew/.linuxbrew/bin/go

echo Rebuilding rootaika binaries in WSL...
wsl -d %WSL_DISTRO% --cd "%REPO_DIR%" -- env GOOS=windows GOARCH=amd64 %GO% build -o dist/rootaika-service.exe ./cmd/rootaika-service
if errorlevel 1 (
    echo Build of rootaika-service failed.
    pause
    exit /b 1
)
wsl -d %WSL_DISTRO% --cd "%REPO_DIR%" -- env GOOS=windows GOARCH=amd64 %GO% build -ldflags "-H=windowsgui" -o dist/rootaika-agent.exe ./cmd/rootaika-agent
if errorlevel 1 (
    echo Build of rootaika-agent failed.
    pause
    exit /b 1
)
echo Build complete.

powershell -ExecutionPolicy Bypass -File "%~dp0test-run.ps1"
pause
