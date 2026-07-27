@echo off
setlocal EnableExtensions
chcp 65001 >nul
set "VERSION=1.19.3"
set "SHA256=5d82148d1032a6a190d9909a2997d9989457aaadf49af87dd02cd4512d31bebe"
set "DEST=%~dp0"
set "ZIP=%TEMP%\mediamtx_%VERSION%_windows_amd64.zip"
set "TMP=%TEMP%\mediamtx_%VERSION%_extract"
set "URL=https://github.com/bluenviron/mediamtx/releases/download/v%VERSION%/mediamtx_v%VERSION%_windows_amd64.zip"

echo Загрузка компонента локальной RTSP-трансляции...
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
  "$ErrorActionPreference='Stop'; [Net.ServicePointManager]::SecurityProtocol=[Net.SecurityProtocolType]::Tls12; Invoke-WebRequest -UseBasicParsing -Uri '%URL%' -OutFile '%ZIP%'; $h=(Get-FileHash -Algorithm SHA256 '%ZIP%').Hash.ToLowerInvariant(); if($h -ne '%SHA256%'){throw ('Неверная контрольная сумма: '+$h)}; if(Test-Path '%TMP%'){Remove-Item '%TMP%' -Recurse -Force}; Expand-Archive -Path '%ZIP%' -DestinationPath '%TMP%' -Force; Copy-Item '%TMP%\mediamtx.exe' '%DEST%\mediamtx.exe' -Force; Remove-Item '%ZIP%' -Force; Remove-Item '%TMP%' -Recurse -Force"

if errorlevel 1 (
  echo.
  echo Не удалось установить компонент локальной RTSP-трансляции.
  pause
  exit /b 1
)

echo Компонент установлен: %DEST%mediamtx.exe
pause
endlocal
