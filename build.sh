#!/bin/bash

# SmartDNSSort Build Script for Unix-like systems
# Usage: ./build.sh [target]
# Targets: windows, linux, all (default: linux)
# 仅支持 x86-64 (amd64) 架构

set -e

# Configuration
BIN_DIR="bin"
MAIN_PATH="./cmd/main.go"
VERSION="${VERSION:-v1.0}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}✓${NC} $*"
}

log_error() {
    echo -e "${RED}✗${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

build_binary() {
    local os=$1
    local arch=$2
    local output=$3
    local desc=$4
    
    log_info "编译 $desc..."
    
    if GOOS="$os" GOARCH="$arch" go build -a -ldflags="-s -w" -o "$output" "$MAIN_PATH" 2>/dev/null; then
        local size=$(du -h "$output" | cut -f1)
        log_success "$desc -> $output ($size)"
        return 0
    else
        log_error "$desc 编译失败"
        return 1
    fi
}

show_help() {
    cat << EOF
SmartDNSSort Build Script

使用方法: ./build.sh [目标]

可用目标:
  linux       - 编译Linux x86-64版本 (默认)
  windows     - 编译Windows x86-64版本
  all         - 编译所有平台 (仅 x86-64)
  clean       - 清理编译文件
  help        - 显示此帮助信息

示例:
  ./build.sh              # 编译Linux x86-64版本
  ./build.sh all          # 编译所有平台
  ./build.sh windows      # 编译Windows x86-64版本

注意: 仅支持 x86-64 (amd64) 架构

EOF
}

# Main
TARGET="${1:-linux}"

echo ""
echo -e "${BLUE}====================================${NC}"
echo -e "${BLUE} SmartDNSSort Build System${NC}"
echo -e "${BLUE} (x86-64 only)${NC}"
echo -e "${BLUE}====================================${NC}"
echo ""

# Check Go
if ! command -v go &> /dev/null; then
    log_error "Go未安装或不在PATH中"
    exit 1
fi

log_info "使用: $(go version)"
echo ""

# Create bin directory
mkdir -p "$BIN_DIR"

# === 前端资源编译 ===
log_info "检查并编译前端资源..."
if [ -f "webapi/web/scripts/setup-all.sh" ]; then
    chmod +x webapi/web/scripts/setup-all.sh
    pushd webapi/web/scripts > /dev/null
    ./setup-all.sh
    popd > /dev/null
    log_success "前端资源处理完成"
else
    log_warn "未找到前端编译脚本 webapi/web/scripts/setup-all.sh"
fi
echo ""

# Execute build
compiled=()

case "$TARGET" in
    linux)
        build_binary "linux" "amd64" "$BIN_DIR/SmartDNSSort-debian-x64" "Linux x86-64" && compiled+=(0) || compiled+=(1)
        ;;
    
    windows)
        build_binary "windows" "amd64" "$BIN_DIR/SmartDNSSort-windows-x64.exe" "Windows x86-64" && compiled+=(0) || compiled+=(1)
        ;;
    
    all)
        build_binary "windows" "amd64" "$BIN_DIR/SmartDNSSort-windows-x64.exe" "Windows x86-64" && compiled+=(0) || compiled+=(1)
        build_binary "linux" "amd64" "$BIN_DIR/SmartDNSSort-debian-x64" "Linux x86-64" && compiled+=(0) || compiled+=(1)
        ;;
    
    clean)
        log_info "清理编译文件..."
        rm -rf "$BIN_DIR"
        go clean
        log_success "清理完成"
        exit 0
        ;;
    
    help)
        show_help
        exit 0
        ;;
    
    *)
        log_error "未知目标: $TARGET"
        echo "使用 '$0 help' 获取帮助"
        exit 1
        ;;
esac

# Show results
echo ""
log_info "输出文件:"
ls -lh "$BIN_DIR/" | tail -n +2 | awk '{printf "  %s (%s)\n", $NF, $5}'

if [[ " ${compiled[@]} " =~ " 1 " ]]; then
    echo ""
    log_error "部分编译失败"
    exit 1
else
    echo ""
    log_success "编译完成！"
    echo ""
    log_info "下一步: 将 $BIN_DIR/ 中的文件上传到 GitHub Releases"
    echo ""
fi
