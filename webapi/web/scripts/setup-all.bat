@echo off
REM SmartDNSSort Complete Setup Script for Windows
REM This script sets up both Tailwind CSS and downloads fonts
REM Run this script from webapi/web directory

echo ======================================
echo SmartDNSSort Complete Setup
echo ======================================
echo.

REM Check if Node.js is installed
node --version >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Node.js is not installed or not in PATH
    echo Please install Node.js from https://nodejs.org/
    pause
    exit /b 1
)

echo [INFO] Node.js version:
node --version
echo.

REM Get the directory where this script is located
set SCRIPT_DIR=%~dp0
set WEB_DIR=%SCRIPT_DIR%..
set CONFIG_DIR=%WEB_DIR%\config

REM Setup CSS
echo [INFO] Installing npm dependencies...
pushd "%CONFIG_DIR%"
call npm install
if errorlevel 1 (
    echo [ERROR] Failed to install dependencies
    popd
    pause
    exit /b 1
)

echo.
echo [INFO] Building Tailwind CSS...
call npm run build
if errorlevel 1 (
    echo [ERROR] Failed to build Tailwind CSS
    popd
    pause
    exit /b 1
)

popd
echo.
echo [SUCCESS] CSS setup complete!
echo.

REM Download Fonts
echo [INFO] Downloading fonts...
echo.

REM Check if Python is available
python --version >nul 2>&1
if errorlevel 1 (
    echo [WARNING] Python not found, trying python3...
    python3 --version >nul 2>&1
    if errorlevel 1 (
        echo [WARNING] Python not available, using batch script instead
        call "%SCRIPT_DIR%download-fonts.bat"
        goto :fonts_done
    ) else (
        python3 "%SCRIPT_DIR%download-fonts.py"
        goto :fonts_done
    )
) else (
    python "%SCRIPT_DIR%download-fonts.py"
)

:fonts_done
echo.
echo ======================================
echo [SUCCESS] Complete setup finished!
echo ======================================
echo.
echo CSS has been built to css/style.css
echo Fonts have been downloaded to fonts/
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
pause
