#!/bin/bash

# SmartDNSSort 快速启动脚本（Linux/macOS）

echo ""
echo "========================================"
echo "    SmartDNSSort DNS Server"
echo "========================================"
echo ""

# 检查 Go 是否安装
if ! command -v go &> /dev/null; then
    echo "[错误] 未检测到 Go 环境，请先安装 Go 1.21+"
    exit 1
fi

echo "[✓] Go 环境检测成功"
echo ""

# 检查配置文件
if [ ! -f "config.yaml" ]; then
    echo "[警告] config.yaml 不存在，请先创建配置文件"
    exit 1
fi

echo "[✓] 配置文件已找到"
echo ""

# 下载依赖
echo "[进行中] 下载依赖..."
go mod tidy
if [ $? -ne 0 ]; then
    echo "[错误] 下载依赖失败"
    exit 1
fi

echo "[✓] 依赖下载完成"
echo ""

# 编译
echo "[进行中] 编译项目..."
go build -o smartdnssort ./cmd
if [ $? -ne 0 ]; then
    echo "[错误] 编译失败"
    exit 1
fi

echo "[✓] 编译成功"
echo ""

# 运行
echo "[开始] 启动 SmartDNSSort DNS Server..."
echo ""
./smartdnssort
