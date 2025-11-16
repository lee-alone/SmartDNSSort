#!/usr/bin/env pwsh
<#
.SYNOPSIS
    SmartDNSSort Build Script
    
.DESCRIPTION
    跨平台编译脚本，支持Windows、Linux、ARM等架构
    
.PARAMETER Target
    编译目标: windows, linux, all (默认: windows)
    
.EXAMPLE
    .\build.ps1 -Target windows
    .\build.ps1 -Target all
    .\build.ps1 linux
#>

param(
    [Parameter(Position=0)]
    [string]$Target = "windows"
)

# 配置
$BinDir = "bin"
$MainPath = ".\cmd\main.go"

# 颜色输出
$Green = "`e[32m"
$Red = "`e[31m"
$Yellow = "`e[33m"
$Blue = "`e[34m"
$Reset = "`e[0m"

function Write-Success {
    Write-Host "$Green✓$Reset $args"
}

function Write-Error2 {
    Write-Host "$Red✗$Reset $args" -ForegroundColor Red
}

function Write-Info {
    Write-Host "$Blue[信息]$Reset $args"
}

function Build-Binary {
    param(
        [string]$OS,
        [string]$Arch,
        [string]$OutputPath,
        [string]$Description
    )
    
    Write-Info "编译 $Description..."
    
    $env:GOOS = $OS
    $env:GOARCH = $Arch
    
    $output = & go build -o $OutputPath $MainPath 2>&1
    
    if ($LASTEXITCODE -eq 0) {
        $size = (Get-Item $OutputPath).Length / 1MB
        Write-Success "$Description -> $OutputPath ({0:F2} MB)" -f $size
        return $true
    }
    else {
        Write-Error2 "$Description 编译失败: $output"
        return $false
    }
}

# 主程序
Write-Host ""
Write-Host "$Blue=====================================$Reset"
Write-Host "$Blue SmartDNSSort Build System$Reset"
Write-Host "$Blue=====================================$Reset"
Write-Host ""

# 检查Go
try {
    $goVersion = go version
    Write-Info "使用: $goVersion"
}
catch {
    Write-Error2 "Go未安装或不在PATH中"
    exit 1
}

# 创建bin目录
if (-not (Test-Path $BinDir)) {
    New-Item -ItemType Directory -Path $BinDir | Out-Null
    Write-Info "创建 $BinDir 目录"
}

Write-Host ""

# 执行编译
$compiled = @()

switch ($Target.ToLower()) {
    "windows" {
        $compiled += @(
            (Build-Binary "windows" "amd64" "$BinDir/SmartDNSSort-windows-x64.exe" "Windows x64"),
            (Build-Binary "windows" "386" "$BinDir/SmartDNSSort-windows-x86.exe" "Windows x86")
        )
    }
    
    "linux" {
        $compiled += @(
            (Build-Binary "linux" "amd64" "$BinDir/SmartDNSSort-debian-x64" "Linux x64"),
            (Build-Binary "linux" "386" "$BinDir/SmartDNSSort-debian-x86" "Linux x86"),
            (Build-Binary "linux" "arm64" "$BinDir/SmartDNSSort-debian-arm64" "Linux ARM64")
        )
    }
    
    "all" {
        $compiled += @(
            (Build-Binary "windows" "amd64" "$BinDir/SmartDNSSort-windows-x64.exe" "Windows x64"),
            (Build-Binary "windows" "386" "$BinDir/SmartDNSSort-windows-x86.exe" "Windows x86"),
            (Build-Binary "linux" "amd64" "$BinDir/SmartDNSSort-debian-x64" "Linux x64"),
            (Build-Binary "linux" "386" "$BinDir/SmartDNSSort-debian-x86" "Linux x86"),
            (Build-Binary "linux" "arm64" "$BinDir/SmartDNSSort-debian-arm64" "Linux ARM64")
        )
    }
    
    "help" {
        Write-Host "使用方法: .\build.ps1 [目标]"
        Write-Host ""
        Write-Host "可用目标:"
        Write-Host "  windows     - 编译Windows版本 (默认)"
        Write-Host "  linux       - 编译Linux版本"
        Write-Host "  all         - 编译所有平台"
        Write-Host "  help        - 显示此帮助信息"
        Write-Host ""
        Write-Host "示例:"
        Write-Host "  .\build.ps1               # 编译Windows版本"
        Write-Host "  .\build.ps1 all           # 编译所有平台"
        Write-Host "  .\build.ps1 -Target linux # 编译Linux版本"
        exit 0
    }
    
    default {
        Write-Error2 "未知目标: $Target"
        Write-Host "使用 '.\build.ps1 help' 获取帮助"
        exit 1
    }
}

# 显示结果
Write-Host ""
Write-Info "输出文件:"
Get-ChildItem -Path $BinDir -Force | ForEach-Object {
    $size = $_.Length / 1MB
    Write-Host "  $Yellow$($_.Name)$Reset ({0:F2} MB)" -f $size
}

if ($compiled -contains $false) {
    Write-Host ""
    Write-Error2 "部分编译失败"
    exit 1
}
else {
    Write-Host ""
    Write-Success "编译完成！"
    Write-Host ""
    Write-Host "下一步: 将 $BinDir/ 中的文件上传到 GitHub Releases"
    Write-Host ""
}
