#!/bin/bash
# SmartDNSSort Tailwind CSS Setup Script for Unix-like systems
# Run this script from webapi/web/scripts directory

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG_DIR="$WEB_DIR/config"

echo "======================================"
echo "SmartDNSSort Tailwind CSS Setup"
echo "======================================"
echo ""

# Check if Node.js is installed
if ! command -v node &> /dev/null; then
    echo "[ERROR] Node.js is not installed or not in PATH"
    echo "Please install Node.js from https://nodejs.org/"
    exit 1
fi

echo "[INFO] Node.js version:"
node --version
echo ""

echo "[INFO] Installing npm dependencies..."
cd "$CONFIG_DIR"
npm install
if [ $? -ne 0 ]; then
    echo "[ERROR] Failed to install dependencies"
    exit 1
fi

echo ""
echo "[SUCCESS] Dependencies installed successfully!"
echo ""
echo "[INFO] Building Tailwind CSS..."
npm run build
if [ $? -ne 0 ]; then
    echo "[ERROR] Failed to build Tailwind CSS"
    exit 1
fi

cd "$WEB_DIR"
echo ""
echo "======================================"
echo "[SUCCESS] Tailwind CSS setup complete!"
echo "======================================"
echo ""
echo "The CSS has been built to css/style.css"
echo "Your HTML file has been updated to use local CSS"
echo ""
echo "To rebuild CSS after making changes, run:"
echo "  cd config"
echo "  npm run build"
echo "  cd .."
echo ""
echo "To watch for changes and auto-rebuild, run:"
echo "  cd config"
echo "  npm run watch"
echo "  cd .."
echo ""
