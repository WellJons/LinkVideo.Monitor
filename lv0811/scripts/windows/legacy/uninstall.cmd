@echo off
setlocal
chcp 65001 >nul
if exist "%~dp0LinkVideo.Monitor.exe" (
  start "" "%~dp0LinkVideo.Monitor.exe" --uninstall
) else (
  echo LinkVideo.Monitor.exe не найден рядом со скриптом удаления.
  pause
)
endlocal
