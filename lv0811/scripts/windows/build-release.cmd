@echo off
setlocal
for %%I in ("%~dp0..\..") do set "ROOT=%%~fI"
call "%ROOT%\scripts\windows\build-app.cmd"
if errorlevel 1 exit /b 1
call "%ROOT%\scripts\windows\build-installer.cmd"
if errorlevel 1 exit /b 1

echo.
echo Release files are in: %ROOT%\build
endlocal
