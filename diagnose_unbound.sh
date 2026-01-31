#!/bin/bash

# Unbound 安装诊断脚本
# 用法: bash diagnose_unbound.sh

echo "=========================================="
echo "Unbound Installation Diagnosis"
echo "=========================================="
echo

# 1. 检查包是否安装
echo "1. Check if unbound package is installed:"
echo "   Command: dpkg -s unbound"
if dpkg -s unbound 2>/dev/null | grep -q "Status: install ok installed"; then
    echo "   ✓ Package is installed"
    dpkg -s unbound | grep -E "^Package:|^Version:|^Status:"
else
    echo "   ✗ Package is NOT installed"
fi
echo

# 2. 列出所有 unbound 文件
echo "2. List all unbound files from package:"
echo "   Command: dpkg -L unbound"
if dpkg -L unbound 2>/dev/null | grep -q "unbound"; then
    echo "   ✓ Files found:"
    dpkg -L unbound 2>/dev/null | grep -E "(bin|sbin)" | head -10
else
    echo "   ✗ No files found"
fi
echo

# 3. 查找 unbound 可执行文件
echo "3. Find unbound executable files:"
echo "   Command: find / -name 'unbound' -type f -executable 2>/dev/null"
FOUND_EXECUTABLES=$(find / -name "unbound" -type f -executable 2>/dev/null | grep -v ".so")
if [ -n "$FOUND_EXECUTABLES" ]; then
    echo "   ✓ Executables found:"
    echo "$FOUND_EXECUTABLES"
else
    echo "   ✗ No executables found"
fi
echo

# 4. 检查标准位置
echo "4. Check standard locations:"
for path in /usr/sbin/unbound /usr/bin/unbound /usr/local/sbin/unbound /usr/local/bin/unbound; do
    if [ -f "$path" ]; then
        echo "   ✓ $path exists"
        ls -lh "$path"
    else
        echo "   ✗ $path not found"
    fi
done
echo

# 5. 检查 PATH
echo "5. Current PATH:"
echo "   $PATH"
echo

# 6. 尝试运行 unbound
echo "6. Try to run 'unbound -V':"
if unbound -V 2>/dev/null; then
    echo "   ✓ Command found in PATH"
else
    echo "   ✗ Command not found in PATH"
fi
echo

# 7. 尝试 which 命令
echo "7. Try 'which unbound':"
if which unbound 2>/dev/null; then
    echo "   ✓ Found via which"
else
    echo "   ✗ Not found via which"
fi
echo

# 8. 尝试 whereis 命令
echo "8. Try 'whereis unbound':"
whereis unbound
echo

# 9. 检查文件权限
echo "9. Check file permissions:"
if [ -f "/usr/sbin/unbound" ]; then
    stat /usr/sbin/unbound
else
    echo "   /usr/sbin/unbound not found"
fi
echo

# 10. 验证包完整性
echo "10. Verify package integrity:"
if command -v debsums &> /dev/null; then
    debsums unbound 2>/dev/null || echo "   Some files may be missing or modified"
else
    echo "   debsums not installed, skipping"
fi
echo

# 11. 检查依赖
echo "11. Check dependencies:"
apt-cache depends unbound 2>/dev/null | head -10
echo

# 12. 总结
echo "=========================================="
echo "Summary:"
echo "=========================================="
if dpkg -s unbound 2>/dev/null | grep -q "Status: install ok installed"; then
    echo "✓ Package is installed in dpkg database"
    if [ -n "$FOUND_EXECUTABLES" ]; then
        echo "✓ Executable found at: $FOUND_EXECUTABLES"
    else
        echo "✗ Executable NOT found on filesystem"
        echo "  This suggests incomplete installation"
        echo "  Try: sudo apt-get install --reinstall unbound"
    fi
else
    echo "✗ Package is NOT installed"
    echo "  Try: sudo apt-get install unbound"
fi
echo
