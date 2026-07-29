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

if not exist "%BUILD%" mkdir "%BUILD%"

cd /d "%ROOT%\installer"
set GOOS=windows
set GOARCH=amd64
set GOAMD64=v1

echo [1/4] Build branded uninstaller...
go build -tags uninstaller -trimpath -ldflags="-s -w -H=windowsgui" -o "%BUILD%\Uninstall.exe" .
if errorlevel 1 exit /b 1

where python >nul 2>nul
if not errorlevel 1 (
  python "%ROOT%\tools\patch_pe_icon.py" "%BUILD%\Uninstall.exe" "%ROOT%\assets\icons\linkvideo_original.ico"
  if errorlevel 1 exit /b 1
)

cd /d "%ROOT%"
if exist "%PAYLOAD_DIR%" rmdir /s /q "%PAYLOAD_DIR%"
mkdir "%PAYLOAD_DIR%"
copy /Y "%BUILD%\LinkVideo.Monitor.exe" "%PAYLOAD_DIR%\LinkVideo.Monitor.exe" >nul
copy /Y "%BUILD%\Uninstall.exe" "%PAYLOAD_DIR%\Uninstall.exe" >nul
copy /Y "%ROOT%\third_party\ffmpeg\bin\ffmpeg.exe" "%PAYLOAD_DIR%\ffmpeg.exe" >nul
copy /Y "%ROOT%\README.md" "%PAYLOAD_DIR%\README.md" >nul
copy /Y "%ROOT%\licenses\LICENSE_RU.txt" "%PAYLOAD_DIR%\LICENSE_RU.txt" >nul
copy /Y "%ROOT%\docs\REMOTE_API.md" "%PAYLOAD_DIR%\REMOTE_API.md" >nul
copy /Y "%ROOT%\docs\TECHNICAL.md" "%PAYLOAD_DIR%\TECHNICAL.md" >nul
copy /Y "%ROOT%\scripts\windows\legacy\install_mediamtx.cmd" "%PAYLOAD_DIR%\install_mediamtx.cmd" >nul

echo [2/4] Create installation payload...
if exist "%PAYLOAD_ZIP%" del /q "%PAYLOAD_ZIP%"
powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "Compress-Archive -Path '%PAYLOAD_DIR%\*' -DestinationPath '%PAYLOAD_ZIP%' -CompressionLevel Optimal -Force"
if errorlevel 1 exit /b 1

echo [3/4] Build branded installer...
cd /d "%ROOT%\installer"
go build -trimpath -ldflags="-s -w -H=windowsgui" -o "%BUILD%\LinkVideo.Monitor_0.8.11_Setup.exe" .
set "RC=%ERRORLEVEL%"
cd /d "%ROOT%"
if not "%RC%"=="0" exit /b %RC%

where python >nul 2>nul
if not errorlevel 1 (
  python "%ROOT%\tools\patch_pe_icon.py" "%BUILD%\LinkVideo.Monitor_0.8.11_Setup.exe" "%ROOT%\assets\icons\linkvideo_original.ico"
  if errorlevel 1 exit /b 1
)

echo [4/4] Cleanup temporary files...
del /q "%PAYLOAD_ZIP%" 2>nul
rmdir /s /q "%PAYLOAD_DIR%" 2>nul

echo Built: %BUILD%\LinkVideo.Monitor_0.8.11_Setup.exe
endlocal
