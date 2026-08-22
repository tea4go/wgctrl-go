@echo off
setlocal

set "REMOTE=root@101.133.133.127"
set "PORT=22"
set "REMOTE_PATH=/opt/bin/wg"
set "OUTPUT=%TEMP%\wg-linux-amd64-%RANDOM%.tmp"

pushd "%~dp0\..\.." || exit /b 1

set "GOOS=linux"
set "GOARCH=amd64"
set "CGO_ENABLED=0"

go build -o "%OUTPUT%" ./cmd/wg
if errorlevel 1 goto :error

ssh -p %PORT% %REMOTE% "mkdir -p /opt/bin"
if errorlevel 1 goto :error

scp -P %PORT% "%OUTPUT%" %REMOTE%:%REMOTE_PATH%
if errorlevel 1 goto :error

ssh -p %PORT% %REMOTE% "chmod 755 %REMOTE_PATH%"
if errorlevel 1 goto :error

del "%OUTPUT%" >nul 2>&1
popd
echo Uploaded Linux amd64 binary to %REMOTE%:%REMOTE_PATH%
exit /b 0

:error
set "EXIT_CODE=%ERRORLEVEL%"
del "%OUTPUT%" >nul 2>&1
popd
exit /b %EXIT_CODE%
