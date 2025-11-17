@echo off
REM SmartDNSSort Build Script for Windows
REM 支持跨平台编�?

setlocal enabledelayedexpansion

echo.
echo ====================================
echo SmartDNSSort Build System
echo ====================================
echo.

REM 检查Go是否安装
go version >nul 2>&1
if %errorlevel% neq 0 (
    echo [错误] Go未安装或不在PATH�?
    exit /b 1
)

REM 默认目标
set TARGET=%1
if "%TARGET%"=="" set TARGET=windows

REM 创建bin目录
if not exist "bin" mkdir bin

echo [信息] 开始编�?..
echo.

REM 编译Windows版本
if "%TARGET%"=="windows" (
    echo [编译] Windows x64...
    setlocal
    set GOOS=windows
    set GOARCH=amd64
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-windows-x64.exe .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-windows-x64.exe
    endlocal
    
    echo [编译] Windows x86...
    setlocal
    set GOOS=windows
    set GOARCH=386
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-windows-x86.exe .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-windows-x86.exe
    endlocal
)

REM 编译Linux版本
if "%TARGET%"=="linux" (
    echo [编译] Linux x64...
    setlocal
    set GOOS=linux
    set GOARCH=amd64
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-debian-x64 .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-debian-x64
    endlocal
    
    echo [编译] Linux x86...
    setlocal
    set GOOS=linux
    set GOARCH=386
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-debian-x86 .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-debian-x86
    endlocal
    
    echo [编译] Linux ARM64...
    setlocal
    set GOOS=linux
    set GOARCH=arm64
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-debian-arm64 .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-debian-arm64
    endlocal
)

REM 编译所有平�?
if "%TARGET%"=="all" (
    echo [编译] Windows x64...
    setlocal
    set GOOS=windows
    set GOARCH=amd64
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-windows-x64.exe .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-windows-x64.exe
    endlocal
    
    echo [编译] Windows x86...
    setlocal
    set GOOS=windows
    set GOARCH=386
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-windows-x86.exe .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-windows-x86.exe
    endlocal
    
    echo [编译] Linux x64...
    setlocal
    set GOOS=linux
    set GOARCH=amd64
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-debian-x64 .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-debian-x64
    endlocal
    
    echo [编译] Linux x86...
    setlocal
    set GOOS=linux
    set GOARCH=386
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-debian-x86 .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-debian-x86
    endlocal
    
    echo [编译] Linux ARM64...
    setlocal
    set GOOS=linux
    set GOARCH=arm64
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-debian-arm64 .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-debian-arm64
    endlocal
)

REM 显示帮助
if "%TARGET%"=="help" (
    echo 使用方法: build.bat [目标]
    echo.
    echo 可用目标:
    echo   windows     - 编译Windows版本 (默认)
    echo   linux       - 编译Linux版本
    echo   all         - 编译所有平�?
    echo   help        - 显示此帮助信�?
    echo.
    echo 示例:
    echo   build.bat              # 编译Windows版本
    echo   build.bat all          # 编译所有平�?
    exit /b 0
)

echo.
echo [成功] 编译完成！输出文件位�? bin\
echo.
dir /B bin\

endlocal
