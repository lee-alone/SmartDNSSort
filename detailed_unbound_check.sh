#!/bin/bash

echo "=========================================="
echo "Detailed Unbound File Search"
echo "=========================================="
echo

echo "1. All files from dpkg -L unbound (bin/sbin only):"
echo "   Command: dpkg -L unbound | grep -E '(bin|sbin)'"
dpkg -L unbound 2>/dev/null | grep -E "(bin|sbin)" | sort
echo

echo "2. Check if each file exists:"
echo "   Checking all bin/sbin files..."
dpkg -L unbound 2>/dev/null | grep -E "(bin|sbin)" | while read file; do
    if [ -f "$file" ]; then
        echo "   ✓ EXISTS: $file"
        echo "     Type: $(file -b "$file")"
    else
        echo "   ✗ MISSING: $file"
    fi
done
echo

echo "3. Find all unbound-related executables:"
echo "   Command: find / -name '*unbound*' -type f -executable 2>/dev/null"
find / -name "*unbound*" -type f -executable 2>/dev/null | head -20
echo

echo "4. Check /etc/init.d/unbound:"
if [ -f "/etc/init.d/unbound" ]; then
    echo "   ✓ File exists"
    echo "   Type: $(file -b /etc/init.d/unbound)"
    echo "   Size: $(stat -f%z /etc/init.d/unbound 2>/dev/null || stat -c%s /etc/init.d/unbound 2>/dev/null) bytes"
    echo "   First 5 lines:"
    head -5 /etc/init.d/unbound | sed 's/^/   /'
else
    echo "   ✗ File not found"
fi
echo

echo "5. All package files (excluding common dirs):"
echo "   Command: dpkg -L unbound | grep -v '^/$' | grep -v '^/etc' | grep -v '^/usr/share' | grep -v '^/var' | grep -v '^/lib'"
dpkg -L unbound 2>/dev/null | grep -v "^/$" | grep -v "^/etc" | grep -v "^/usr/share" | grep -v "^/var" | grep -v "^/lib" | sort
echo

echo "6. Check /usr/sbin/unbound specifically:"
if [ -f "/usr/sbin/unbound" ]; then
    echo "   ✓ /usr/sbin/unbound exists"
    /usr/sbin/unbound -V
else
    echo "   ✗ /usr/sbin/unbound NOT found"
fi
echo

echo "7. Check /usr/bin/unbound specifically:"
if [ -f "/usr/bin/unbound" ]; then
    echo "   ✓ /usr/bin/unbound exists"
    /usr/bin/unbound -V
else
    echo "   ✗ /usr/bin/unbound NOT found"
fi
echo

echo "8. Try to run unbound from PATH:"
if command -v unbound &> /dev/null; then
    echo "   ✓ unbound found in PATH"
    unbound -V
else
    echo "   ✗ unbound NOT found in PATH"
fi
echo

echo "9. Package status:"
dpkg -s unbound 2>/dev/null | grep -E "^Package:|^Version:|^Status:|^Architecture:"
echo

echo "=========================================="
echo "Summary:"
echo "=========================================="
if [ -f "/usr/sbin/unbound" ]; then
    echo "✓ /usr/sbin/unbound exists - USE THIS PATH"
elif [ -f "/usr/bin/unbound" ]; then
    echo "✓ /usr/bin/unbound exists - USE THIS PATH"
elif command -v unbound &> /dev/null; then
    echo "✓ unbound found in PATH at: $(which unbound)"
else
    FOUND=$(find / -name "unbound" -type f -executable 2>/dev/null | grep -v ".so" | head -1)
    if [ -n "$FOUND" ]; then
        echo "✓ unbound found at: $FOUND"
    else
        echo "✗ unbound binary NOT found anywhere"
        echo "  This is a package installation problem"
        echo "  Try: sudo apt-get install --reinstall unbound"
    fi
fi
