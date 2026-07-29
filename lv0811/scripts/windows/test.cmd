@echo off
setlocal
for %%I in ("%~dp0..\..") do set "ROOT=%%~fI"
cd /d "%ROOT%"
go test ./cmd/linkvideo-monitor
endlocal
