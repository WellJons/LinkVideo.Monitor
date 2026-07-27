@echo off
setlocal EnableExtensions EnableDelayedExpansion
chcp 65001 >nul
set "DEST=%LOCALAPPDATA%\Programs\LinkVideo.Monitor"
set "STARTMENU=%APPDATA%\Microsoft\Windows\Start Menu\Programs"
set "APP=%DEST%\LinkVideo.Monitor.exe"

echo Установка LinkVideo Monitor 0.4.7...

if not exist "%~dp0LinkVideo.Monitor.exe" (
  echo ОШИБКА: LinkVideo.Monitor.exe отсутствует рядом с установщиком.
  echo Проверьте журнал защиты Windows: файл мог быть помещён в карантин.
  pause
  exit /b 2
)

rem Останавливаем главное приложение и всё его дерево: FFmpeg, overlay и аудиомодуль.
taskkill /IM LinkVideo.Monitor.exe /T /F >nul 2>nul
taskkill /IM LinkVideo.ScreenOverlay.exe /T /F >nul 2>nul
taskkill /IM LinkVideo.AudioLoopback.exe /T /F >nul 2>nul

rem Windows иногда освобождает EXE не мгновенно. Ждём до 10 секунд.
for /L %%I in (1,1,20) do (
  set "BUSY="
  tasklist /FI "IMAGENAME eq LinkVideo.Monitor.exe" 2>nul | find /I "LinkVideo.Monitor.exe" >nul && set "BUSY=1"
  tasklist /FI "IMAGENAME eq LinkVideo.ScreenOverlay.exe" 2>nul | find /I "LinkVideo.ScreenOverlay.exe" >nul && set "BUSY=1"
  tasklist /FI "IMAGENAME eq LinkVideo.AudioLoopback.exe" 2>nul | find /I "LinkVideo.AudioLoopback.exe" >nul && set "BUSY=1"
  if not defined BUSY goto processes_stopped
  >nul ping 127.0.0.1 -n 2
)

:processes_stopped
if not exist "%DEST%" mkdir "%DEST%"

copy /Y "%~dp0LinkVideo.Monitor.exe" "%DEST%\LinkVideo.Monitor.new" >nul || goto copy_error
copy /Y "%~dp0LinkVideo.ScreenOverlay.exe" "%DEST%\LinkVideo.ScreenOverlay.new" >nul || goto copy_error
copy /Y "%~dp0LinkVideo.AudioLoopback.exe" "%DEST%\LinkVideo.AudioLoopback.new" >nul || goto copy_error

move /Y "%DEST%\LinkVideo.Monitor.new" "%DEST%\LinkVideo.Monitor.exe" >nul || goto copy_error
move /Y "%DEST%\LinkVideo.ScreenOverlay.new" "%DEST%\LinkVideo.ScreenOverlay.exe" >nul || goto copy_error
move /Y "%DEST%\LinkVideo.AudioLoopback.new" "%DEST%\LinkVideo.AudioLoopback.exe" >nul || goto copy_error

copy /Y "%~dp0README.md" "%DEST%\README.md" >nul 2>nul
copy /Y "%~dp0CHANGELOG.md" "%DEST%\CHANGELOG.md" >nul 2>nul
copy /Y "%~dp0TECHNICAL.md" "%DEST%\TECHNICAL.md" >nul 2>nul
copy /Y "%~dp0install_mediamtx.cmd" "%DEST%\install_mediamtx.cmd" >nul 2>nul
copy /Y "%~dp0uninstall.cmd" "%DEST%\uninstall.cmd" >nul 2>nul
if exist "%~dp0mediamtx.exe" copy /Y "%~dp0mediamtx.exe" "%DEST%\mediamtx.exe" >nul

set "FFSRC="
if exist "%ProgramFiles(x86)%\LinkVideo.Monitor\ffmpeg_real.exe" set "FFSRC=%ProgramFiles(x86)%\LinkVideo.Monitor\ffmpeg_real.exe"
if "%FFSRC%"=="" if exist "%ProgramFiles(x86)%\LinkVideo.Monitor\ffmpeg.exe" set "FFSRC=%ProgramFiles(x86)%\LinkVideo.Monitor\ffmpeg.exe"
if "%FFSRC%"=="" if exist "%ProgramFiles%\LinkVideo.Monitor\ffmpeg_real.exe" set "FFSRC=%ProgramFiles%\LinkVideo.Monitor\ffmpeg_real.exe"
if "%FFSRC%"=="" if exist "%ProgramFiles%\LinkVideo.Monitor\ffmpeg.exe" set "FFSRC=%ProgramFiles%\LinkVideo.Monitor\ffmpeg.exe"

if not "%FFSRC%"=="" (
  echo Копирование FFmpeg из установленного LinkVideo.Monitor...
  copy /Y "%FFSRC%" "%DEST%\ffmpeg.exe" >nul
) else if exist "%~dp0ffmpeg.exe" (
  copy /Y "%~dp0ffmpeg.exe" "%DEST%\ffmpeg.exe" >nul
) else (
  echo ВНИМАНИЕ: FFmpeg не найден. Переустановите программу полным установщиком, содержащим FFmpeg.
)

powershell -NoProfile -ExecutionPolicy Bypass -Command "$w=New-Object -ComObject WScript.Shell;$s=$w.CreateShortcut('%STARTMENU%\LinkVideo Monitor.lnk');$s.TargetPath='%APP%';$s.WorkingDirectory='%DEST%';$s.Save()" >nul 2>nul
reg add "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v "LinkVideo Monitor" /t REG_SZ /d "\"%APP%\" --background" /f >nul 2>nul

echo.
echo Готово: %DEST%
start "" "%APP%"
exit /b 0

:copy_error
echo.
echo ОШИБКА: Windows не разрешила заменить файл программы.
echo 1. Убедитесь, что LinkVideo Monitor закрыт.
echo 2. Откройте "Безопасность Windows" - "Журнал защиты".
echo 3. Проверьте, не помещён ли EXE в карантин.
pause
exit /b 3
