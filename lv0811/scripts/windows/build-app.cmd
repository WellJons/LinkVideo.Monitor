@echo off
setlocal EnableExtensions
for %%I in ("%~dp0..\..") do set "ROOT=%%~fI"
set "BUILD=%ROOT%\build"

if not exist "%BUILD%" mkdir "%BUILD%"
cd /d "%ROOT%"

set GOOS=windows
set GOARCH=amd64
set GOAMD64=v1

set "REMOTE_LDFLAGS="
if defined LINKVIDEO_REMOTE_API_URL set "REMOTE_LDFLAGS=%REMOTE_LDFLAGS% -X main.defaultRemoteAPIURL=%LINKVIDEO_REMOTE_API_URL%"
if defined LINKVIDEO_REMOTE_API_KEY set "REMOTE_LDFLAGS=%REMOTE_LDFLAGS% -X main.defaultRemoteAPIKey=%LINKVIDEO_REMOTE_API_KEY%"
if defined LINKVIDEO_ADMIN_PASSWORD_SHA256 set "REMOTE_LDFLAGS=%REMOTE_LDFLAGS% -X main.embeddedAdminPasswordSHA256=%LINKVIDEO_ADMIN_PASSWORD_SHA256%"

echo [1/3] Tests...
go test ./cmd/linkvideo-monitor
if errorlevel 1 exit /b 1

echo [2/3] Build application...
go build -trimpath -ldflags="-s -w -H=windowsgui%REMOTE_LDFLAGS%" -o "%BUILD%\LinkVideo.Monitor.exe" ./cmd/linkvideo-monitor
if errorlevel 1 exit /b 1

echo [3/3] Apply icon...
where python >nul 2>nul
if not errorlevel 1 (
  python "%ROOT%\tools\patch_pe_icon.py" "%BUILD%\LinkVideo.Monitor.exe" "%ROOT%\assets\icons\linkvideo_original.ico"
  if errorlevel 1 exit /b 1
) else (
  echo WARNING: Python not found, icon patch skipped.
)

echo Built: %BUILD%\LinkVideo.Monitor.exe
if not defined LINKVIDEO_REMOTE_API_URL echo WARNING: Remote API URL is not embedded; remote control will remain disabled.
endlocal
