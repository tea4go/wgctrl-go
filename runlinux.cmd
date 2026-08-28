@echo off
setlocal

set "SCRIPT_DIR=%~dp0"
set "PS1_SCRIPT=%SCRIPT_DIR%buildapp.ps1"

if not exist "%PS1_SCRIPT%" (
    echo [ERROR] buildapp.ps1 not found: %PS1_SCRIPT%
    exit /b 1
)

REM Thin wrapper: delegate Linux cross-build to buildapp.ps1, forcing -OS linux.
REM Usage:
REM   runlinux.cmd                 -> default linux/amd64
REM   runlinux.cmd -Arch arm64     -> linux/arm64
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%PS1_SCRIPT%" -OS linux %*

scp -P 22   wgc root@101.133.133.127:/opt/wireguard/
rem scp -P 22   wgd root@101.133.133.127:/opt/wireguard/
rem scp -P 6443 wgc root@8.210.19.98:/opt/wireguard/
rem scp -P 6443 wgd root@8.210.19.98:/opt/wireguard/

ssh root@101.133.133.127 "export gitee_token='%gitee_token%' && /opt/wireguard/wgc -l=7 syncgitee wgtest"

exit /b %ERRORLEVEL%
