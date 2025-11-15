#!/bin/bash

# SmartDNSSort 安装脚本
# 用法: sudo ./install.sh [选项]
#
# 选项:
#   -h, --help          显示帮助信息
#   -c, --config PATH   指定配置文件路径
#   -w, --work DIR      指定工作目录
#   -u, --user USER     指定运行用户（默认 root）
#   --dry-run           预览安装流程
#   -v, --verbose       详细输出

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 默认配置
CONFIG_PATH=""
WORK_DIR=""
RUN_USER=""
DRY_RUN=false
VERBOSE=false

# 打印帮助信息
usage() {
    cat << EOF
SmartDNSSort 安装脚本

使用方法:
    sudo ./install.sh [选项]

选项:
    -h, --help          显示此帮助信息
    -c, --config PATH   指定配置文件路径 (默认: /etc/SmartDNSSort/config.yaml)
    -w, --work DIR      指定工作目录 (默认: /var/lib/SmartDNSSort)
    -u, --user USER     指定运行用户 (默认: root)
    --dry-run           预览安装流程，不实际执行
    -v, --verbose       详细输出

示例:
    # 默认安装 (使用 root 用户)
    sudo ./install.sh

    # 指定自定义配置路径
    sudo ./install.sh -c /custom/path/config.yaml

    # 预览安装流程
    sudo ./install.sh --dry-run

EOF
    exit 0
}

# 打印信息
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[✓]${NC} $1"
}

log_error() {
    echo -e "${RED}[✗]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[!]${NC} $1"
}

log_dry_run() {
    echo -e "${YELLOW}[DRY-RUN]${NC} $1"
}

# 解析命令行参数
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                usage
                ;;
            -c|--config)
                CONFIG_PATH="$2"
                shift 2
                ;;
            -w|--work)
                WORK_DIR="$2"
                shift 2
                ;;
            -u|--user)
                RUN_USER="$2"
                shift 2
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            *)
                log_error "未知选项: $1"
                echo "使用 -h 或 --help 查看帮助信息"
                exit 1
                ;;
        esac
    done
}

# 主函数
main() {
    parse_args "$@"
    
    echo "============================================"
    echo "SmartDNSSort 安装程序"
    echo "============================================"
    echo ""
    
    if [ "$DRY_RUN" = true ]; then
        log_warn "干运行模式：仅预览，不实际执行任何操作"
        echo ""
    fi
    
    # 设置参数
    [ -z "$CONFIG_PATH" ] && CONFIG_PATH="/etc/SmartDNSSort/config.yaml"
    [ -z "$WORK_DIR" ] && WORK_DIR="/var/lib/SmartDNSSort"
    [ -z "$RUN_USER" ] && RUN_USER="root"
    
    if [ "$VERBOSE" = true ]; then
        log_info "配置文件路径: $CONFIG_PATH"
        log_info "工作目录: $WORK_DIR"
        log_info "运行用户: $RUN_USER"
        echo ""
    fi
    
    # 调用 SmartDNSSort 的 -s install 命令
    local cmd="./SmartDNSSort -s install -c \"$CONFIG_PATH\" -w \"$WORK_DIR\" -user \"$RUN_USER\""
    
    if [ "$DRY_RUN" = true ]; then
        cmd="$cmd --dry-run"
    fi
    
    if [ "$VERBOSE" = true ]; then
        cmd="$cmd -v"
    fi
    
    if [ "$VERBOSE" = true ]; then
        log_info "执行命令: $cmd"
        echo ""
    fi
    
    eval "$cmd"
    
    if [ $? -eq 0 ]; then
        if [ "$DRY_RUN" = false ]; then
            echo ""
            log_success "安装完成！"
            echo ""
            echo "后续步骤:"
            echo "  1. 编辑配置文件: sudo nano $CONFIG_PATH"
            echo "  2. 查看服务状态: sudo systemctl status SmartDNSSort"
            echo "  3. 查看实时日志: sudo journalctl -u SmartDNSSort -f"
            echo ""
            echo "卸载服务: sudo ./install.sh -s uninstall"
        fi
    else
        log_error "安装失败，请查看上面的错误信息"
        exit 1
    fi
}

main "$@"
