@echo off
REM SmartDNSSort Build Script for Windows
REM 仅支持 x86-64 (amd64) 架构

setlocal enabledelayedexpansion

echo.
echo ====================================
echo SmartDNSSort Build System
echo (x86-64 only)
echo ====================================
echo.

REM 检查Go是否安装
go version >nul 2>&1
if %errorlevel% neq 0 (
    echo [错误] Go未安装或不在PATH中
    exit /b 1
)

REM 默认目标
set TARGET=%1
if "%TARGET%"=="" set TARGET=windows

REM === 前端资源编译 ===
echo [信息] 检查并编译前端资源...
set FRONTEND_SCRIPT=webapi\web\scripts\setup-all.bat

if not exist "%FRONTEND_SCRIPT%" (
    echo [错误] 未找到前端编译脚本: %FRONTEND_SCRIPT%
    echo [信息] 请确保已正确安装前端构建工具
    exit /b 1
)

REM 保存当前目录
set CURRENT_DIR=%CD%
pushd "webapi\web\scripts" 2>nul
if errorlevel 1 (
    echo [错误] 无法切换到前端脚本目录
    exit /b 1
)

REM 使用非交互模式运行 setup-all.bat (设置 NO_PAUSE=1 抑制 pause)
set NO_PAUSE=1
call setup-all.bat
set BUILD_ERROR=%errorlevel%

if %BUILD_ERROR% neq 0 (
    echo [错误] 前端资源编译失败，错误代码: %BUILD_ERROR%
    popd
    exit /b %BUILD_ERROR%
)

popd 2>nul
echo [信息] 前端资源处理完成。
echo.

REM 创建bin目录
if not exist "bin" mkdir bin

echo [信息] 开始编译...
echo.

REM 编译Windows x86-64版本
if "%TARGET%"=="windows" (
    echo [编译] Windows x86-64...
    setlocal
    set GOOS=windows
    set GOARCH=amd64
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-windows-x64.exe .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-windows-x64.exe
    endlocal
)

REM 编译Linux x86-64版本
if "%TARGET%"=="linux" (
    echo [编译] Linux x86-64...
    setlocal
    set GOOS=linux
    set GOARCH=amd64
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-debian-x64 .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-debian-x64
    endlocal
)

REM 编译所有平台
if "%TARGET%"=="all" (
    echo [编译] Windows x86-64...
    setlocal
    set GOOS=windows
    set GOARCH=amd64
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-windows-x64.exe .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-windows-x64.exe
    endlocal
    
    echo [编译] Linux x86-64...
    setlocal
    set GOOS=linux
    set GOARCH=amd64
    go build -a -ldflags="-s -w" -o bin\SmartDNSSort-debian-x64 .\cmd\main.go
    if !errorlevel! equ 0 echo [完成] bin\SmartDNSSort-debian-x64
    endlocal
)

echo.
echo [成功] 编译完成！输出文件位置: bin\
echo.
dir /B bin\

endlocal
