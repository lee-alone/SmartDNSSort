@echo off
REM SmartDNSSort 快速启动脚本

echo.
echo ========================================
echo    SmartDNSSort DNS Server
echo ========================================
echo.

REM 检查 Go 是否安装
where go >nul 2>nul
if %ERRORLEVEL% NEQ 0 (
    echo [错误] 未检测到 Go 环境，请先安装 Go 1.21+
    pause
    exit /b 1
)

echo [✓] Go 环境检测成功
echo.

REM 检查配置文件
if not exist "config.yaml" (
    echo [警告] config.yaml 不存在，请先创建配置文件
    pause
    exit /b 1
)

echo [✓] 配置文件已找到
echo.

REM 下载依赖
echo [进行中] 下载依赖...
call go mod tidy
if %ERRORLEVEL% NEQ 0 (
    echo [错误] 下载依赖失败
    pause
    exit /b 1
)

echo [✓] 依赖下载完成
echo.

REM 编译
echo [进行中] 编译项目...
call go build -o smartdnssort.exe ./cmd
if %ERRORLEVEL% NEQ 0 (
    echo [错误] 编译失败
    pause
    exit /b 1
)

echo [✓] 编译成功
echo.

REM 运行
echo [开始] 启动 SmartDNSSort DNS Server...
echo.
call smartdnssort.exe

pause
