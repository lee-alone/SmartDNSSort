#!/bin/bash

# SmartDNSSort Linux 安装功能测试脚本
# 注意：此脚本需要在 Linux 系统上运行并拥有 sudo 权限

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 测试计数
TESTS_PASSED=0
TESTS_FAILED=0

# 日志函数
log_test() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

log_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((TESTS_FAILED++))
}

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# 清理函数
cleanup() {
    log_info "清理测试环境..."
    sudo systemctl stop SmartDNSSort 2>/dev/null || true
    sudo systemctl disable SmartDNSSort 2>/dev/null || true
    sudo rm -rf /etc/SmartDNSSort
    sudo rm -rf /var/lib/SmartDNSSort
    sudo rm -rf /var/log/SmartDNSSort
    sudo rm -f /usr/local/bin/SmartDNSSort
    sudo rm -f /etc/systemd/system/SmartDNSSort.service
    sudo systemctl daemon-reload
}

# 测试：检查二进制文件存在
test_binary_exists() {
    log_test "检查二进制文件是否存在"
    if [ -f "./SmartDNSSort" ]; then
        log_pass "二进制文件存在"
    else
        log_fail "二进制文件不存在"
        exit 1
    fi
}

# 测试：检查帮助信息
test_help_info() {
    log_test "检查帮助信息"
    if ./SmartDNSSort -h | grep -q "SmartDNSSort"; then
        log_pass "帮助信息正确显示"
    else
        log_fail "帮助信息显示失败"
    fi
}

# 测试：干运行安装
test_dry_run_install() {
    log_test "测试干运行安装"
    if sudo ./SmartDNSSort -s install --dry-run 2>&1 | grep -q "DRY-RUN"; then
        log_pass "干运行安装预览成功"
    else
        log_fail "干运行安装预览失败"
    fi
}

# 测试：实际安装
test_install() {
    log_test "测试实际安装"
    if sudo ./SmartDNSSort -s install -v 2>&1 | grep -q "已成功安装"; then
        log_pass "安装成功"
    else
        log_fail "安装失败"
        return 1
    fi
}

# 测试：检查配置文件
test_config_created() {
    log_test "检查配置文件是否创建"
    if [ -f "/etc/SmartDNSSort/config.yaml" ]; then
        log_pass "配置文件已创建"
    else
        log_fail "配置文件未创建"
        return 1
    fi
}

# 测试：检查二进制复制
test_binary_copied() {
    log_test "检查二进制是否复制到系统目录"
    if [ -f "/usr/local/bin/SmartDNSSort" ]; then
        log_pass "二进制已复制"
    else
        log_fail "二进制未复制"
        return 1
    fi
}

# 测试：检查服务文件
test_service_file() {
    log_test "检查 systemd 服务文件是否创建"
    if [ -f "/etc/systemd/system/SmartDNSSort.service" ]; then
        log_pass "服务文件已创建"
    else
        log_fail "服务文件未创建"
        return 1
    fi
}

# 测试：检查服务状态
test_service_status() {
    log_test "检查服务是否运行"
    
    # 等待服务启动
    sleep 2
    
    if sudo systemctl is-active SmartDNSSort | grep -q "active"; then
        log_pass "服务运行正常"
    else
        log_warn "服务未运行，查看日志："
        sudo journalctl -u SmartDNSSort -n 20 --no-pager | head -20
        log_fail "服务状态异常"
    fi
}

# 测试：DNS 端口监听
test_dns_port() {
    log_test "检查 DNS 端口是否监听"
    
    sleep 1
    
    if sudo netstat -ulnp 2>/dev/null | grep -q ":53.*LISTEN" || \
       sudo ss -ulnp 2>/dev/null | grep -q ":53.*LISTEN"; then
        log_pass "DNS 端口 53 已监听"
    else
        log_warn "DNS 端口可能未监听（可能需要时间启动）"
    fi
}

# 测试：查询状态
test_query_status() {
    log_test "测试状态查询命令"
    if ./SmartDNSSort -s status 2>&1 | grep -q "SmartDNSSort"; then
        log_pass "状态查询成功"
    else
        log_fail "状态查询失败"
    fi
}

# 测试：干运行卸载
test_dry_run_uninstall() {
    log_test "测试干运行卸载"
    if sudo ./SmartDNSSort -s uninstall --dry-run 2>&1 | grep -q "DRY-RUN"; then
        log_pass "干运行卸载预览成功"
    else
        log_fail "干运行卸载预览失败"
    fi
}

# 测试：卸载
test_uninstall() {
    log_test "测试卸载"
    if sudo ./SmartDNSSort -s uninstall 2>&1 | grep -q "已成功卸载"; then
        log_pass "卸载成功"
    else
        log_fail "卸载失败"
        return 1
    fi
}

# 测试：检查卸载清理
test_uninstall_cleanup() {
    log_test "检查卸载是否完全清理"
    
    local failed=false
    
    if [ -f "/etc/systemd/system/SmartDNSSort.service" ]; then
        log_warn "服务文件未删除"
        failed=true
    fi
    
    if [ -f "/usr/local/bin/SmartDNSSort" ]; then
        log_warn "二进制文件未删除"
        failed=true
    fi
    
    if [ -d "/etc/SmartDNSSort" ]; then
        log_warn "配置目录未删除"
        failed=true
    fi
    
    if [ "$failed" = true ]; then
        log_fail "卸载清理不完整"
        return 1
    else
        log_pass "卸载清理完整"
    fi
}

# 主测试流程
main() {
    echo "=================================================="
    echo "SmartDNSSort Linux 安装功能测试"
    echo "=================================================="
    echo ""
    
    # 检查权限
    if [ "$EUID" -ne 0 ]; then
        log_fail "此测试需要 root 权限"
        echo "请使用 sudo 运行此脚本："
        echo "  sudo ./test.sh"
        exit 1
    fi
    
    # 检查系统要求
    log_test "检查系统要求"
    if ! command -v systemctl &> /dev/null; then
        log_fail "系统不支持 systemd"
        exit 1
    fi
    log_pass "系统支持 systemd"
    
    echo ""
    
    # 运行测试
    log_info "========== 阶段 1: 基础检查 =========="
    test_binary_exists
    test_help_info
    
    echo ""
    log_info "========== 阶段 2: 干运行测试 =========="
    test_dry_run_install
    
    echo ""
    log_info "========== 阶段 3: 清理环境 =========="
    cleanup
    
    echo ""
    log_info "========== 阶段 4: 安装测试 =========="
    test_install
    test_config_created
    test_binary_copied
    test_service_file
    test_service_status
    test_dns_port
    test_query_status
    
    echo ""
    log_info "========== 阶段 5: 卸载测试 =========="
    test_dry_run_uninstall
    
    echo ""
    log_info "========== 阶段 6: 执行卸载 =========="
    test_uninstall
    test_uninstall_cleanup
    
    echo ""
    echo "=================================================="
    echo "测试完成"
    echo "=================================================="
    echo -e "通过: ${GREEN}$TESTS_PASSED${NC} | 失败: ${RED}$TESTS_FAILED${NC}"
    echo ""
    
    if [ $TESTS_FAILED -eq 0 ]; then
        log_pass "所有测试通过！✓"
        exit 0
    else
        log_fail "有 $TESTS_FAILED 个测试失败"
        exit 1
    fi
}

# 运行主函数
main "$@"
