@echo off
setlocal EnableExtensions
for %%I in ("%~dp0..\..") do set "ROOT=%%~fI"
set "BUILD=%ROOT%\build"
set "PAYLOAD_DIR=%BUILD%\payload"
set "PAYLOAD_ZIP=%ROOT%\installer\payload.zip"

if not exist "%BUILD%\LinkVideo.Monitor.exe" (
  call "%ROOT%\scripts\windows\build-app.cmd"
  if errorlevel 1 exit /b 1
)

if exist "%PAYLOAD_DIR%" rmdir /s /q "%PAYLOAD_DIR%"
mkdir "%PAYLOAD_DIR%"
copy /Y "%BUILD%\LinkVideo.Monitor.exe" "%PAYLOAD_DIR%\LinkVideo.Monitor.exe" >nul
copy /Y "%ROOT%\third_party\ffmpeg\bin\ffmpeg.exe" "%PAYLOAD_DIR%\ffmpeg.exe" >nul
copy /Y "%ROOT%\README.md" "%PAYLOAD_DIR%\README.md" >nul
copy /Y "%ROOT%\licenses\LICENSE_RU.txt" "%PAYLOAD_DIR%\LICENSE_RU.txt" >nul
copy /Y "%ROOT%\docs\REMOTE_API.md" "%PAYLOAD_DIR%\REMOTE_API.md" >nul
copy /Y "%ROOT%\docs\TECHNICAL.md" "%PAYLOAD_DIR%\TECHNICAL.md" >nul
copy /Y "%ROOT%\scripts\windows\legacy\install_mediamtx.cmd" "%PAYLOAD_DIR%\install_mediamtx.cmd" >nul

if exist "%PAYLOAD_ZIP%" del /q "%PAYLOAD_ZIP%"
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "Compress-Archive -Path '%PAYLOAD_DIR%\*' -DestinationPath '%PAYLOAD_ZIP%' -CompressionLevel Optimal -Force"
if errorlevel 1 exit /b 1

cd /d "%ROOT%\installer"
set GOOS=windows
set GOARCH=amd64
set GOAMD64=v1
go build -trimpath -ldflags="-s -w -H=windowsgui" -o "%BUILD%\LinkVideo.Monitor_0.7.10_Setup.exe" .
set "RC=%ERRORLEVEL%"
cd /d "%ROOT%"

del /q "%PAYLOAD_ZIP%" 2>nul
rmdir /s /q "%PAYLOAD_DIR%" 2>nul

if not "%RC%"=="0" exit /b %RC%
echo Built: %BUILD%\LinkVideo.Monitor_0.7.10_Setup.exe
endlocal
