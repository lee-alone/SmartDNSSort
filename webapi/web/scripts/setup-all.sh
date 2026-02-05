#!/bin/bash
# SmartDNSSort Complete Setup Script for Unix-like systems
# This script sets up both Tailwind CSS and downloads fonts
# Run this script from webapi/web/scripts directory

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG_DIR="$WEB_DIR/config"

echo "======================================"
echo "SmartDNSSort Complete Setup"
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

# Setup CSS
echo "[INFO] Installing npm dependencies..."
cd "$CONFIG_DIR"
npm install
if [ $? -ne 0 ]; then
    echo "[ERROR] Failed to install dependencies"
    exit 1
fi

echo ""
echo "[INFO] Building Tailwind CSS..."
npm run build
if [ $? -ne 0 ]; then
    echo "[ERROR] Failed to build Tailwind CSS"
    exit 1
fi

cd "$WEB_DIR"
echo ""
echo "[SUCCESS] CSS setup complete!"
echo ""

# Download Fonts
echo "[INFO] Downloading fonts..."
echo ""

# Try Python first (preferred)
if command -v python3 &> /dev/null; then
    python3 "$SCRIPT_DIR/download-fonts.py"
elif command -v python &> /dev/null; then
    python "$SCRIPT_DIR/download-fonts.py"
else
    echo "[WARNING] Python not found, using shell script instead"
    chmod +x "$SCRIPT_DIR/download-fonts.sh"
    "$SCRIPT_DIR/download-fonts.sh"
fi

echo ""
echo "======================================"
echo "[SUCCESS] Complete setup finished!"
echo "======================================"
echo ""
echo "CSS has been built to css/style.css"
echo "Fonts have been downloaded to fonts/"
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
