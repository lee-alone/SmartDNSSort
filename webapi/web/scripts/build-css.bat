@echo off
REM Simple CSS build script for integration with main build system
REM Run this script from webapi/web/scripts directory

set SCRIPT_DIR=%~dp0
set WEB_DIR=%SCRIPT_DIR%..
set CONFIG_DIR=%WEB_DIR%\config

pushd "%CONFIG_DIR%"

REM Check if node_modules exists
if not exist "node_modules" (
    echo [INFO] Node modules not found, installing dependencies...
    call npm install --silent
)

REM Build CSS
echo [INFO] Building Tailwind CSS...
call npm run build

if errorlevel 1 (
    echo [ERROR] CSS build failed
    popd
    exit /b 1
) else (
    echo [SUCCESS] CSS build completed
    popd
)
