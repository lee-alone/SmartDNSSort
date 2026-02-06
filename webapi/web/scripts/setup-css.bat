@echo off
REM SmartDNSSort Tailwind CSS Setup Script for Windows
REM Run this script from webapi/web/scripts directory

echo ======================================
echo SmartDNSSort Tailwind CSS Setup
echo ======================================
echo.

REM Check if Node.js is installed
node --version >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Node.js is not installed or not in PATH
    echo Please install Node.js from https://nodejs.org/
    if not defined NO_PAUSE pause
    exit /b 1
)

echo [INFO] Node.js version:
node --version
echo.

REM Get the directory where this script is located
set SCRIPT_DIR=%~dp0
set WEB_DIR=%SCRIPT_DIR%..
set CONFIG_DIR=%WEB_DIR%\config

echo [INFO] Installing npm dependencies...
pushd "%CONFIG_DIR%"
call npm install
if errorlevel 1 (
    echo [ERROR] Failed to install dependencies
    popd
    if not defined NO_PAUSE pause
    exit /b 1
)

echo.
echo [SUCCESS] Dependencies installed successfully!
echo.
echo [INFO] Building Tailwind CSS...
call npm run build
if errorlevel 1 (
    echo [ERROR] Failed to build Tailwind CSS
    popd
    if not defined NO_PAUSE pause
    exit /b 1
)

popd
echo.
echo ======================================
echo [SUCCESS] Tailwind CSS setup complete!
echo ======================================
echo.
echo The CSS has been built to css/style.css
echo Your HTML file has been updated to use local CSS
echo.
echo To rebuild CSS after making changes, run:
echo   cd config
echo   npm run build
echo   cd ..
echo.
echo To watch for changes and auto-rebuild, run:
echo   cd config
echo   npm run watch
echo   cd ..
echo.
if not defined NO_PAUSE pause
