#!/bin/bash
# Simple CSS build script for integration with main build system
# Run this script from webapi/web/scripts directory

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG_DIR="$WEB_DIR/config"

cd "$CONFIG_DIR"

# Check if node_modules exists
if [ ! -d "node_modules" ]; then
    echo "[INFO] Node modules not found, installing dependencies..."
    npm install --silent
fi

# Build CSS
echo "[INFO] Building Tailwind CSS..."
npm run build

if [ $? -eq 0 ]; then
    echo "[SUCCESS] CSS build completed"
else
    echo "[ERROR] CSS build failed"
    exit 1
fi
