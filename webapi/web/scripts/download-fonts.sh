#!/bin/bash
# Download Google Fonts locally for offline use

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$(dirname "$SCRIPT_DIR")"
FONTS_DIR="$WEB_DIR/fonts"

mkdir -p "$FONTS_DIR"

echo "Downloading Spline Sans fonts..."

# Spline Sans weights
declare -a weights=(300 400 500 600 700)

for weight in "${weights[@]}"; do
    echo "Downloading Spline Sans $weight..."
    # Using Google Fonts API to get the actual font file URLs
    curl -s "https://fonts.googleapis.com/css2?family=Spline+Sans:wght@$weight&display=swap" \
        -H "User-Agent: Mozilla/5.0" | grep -oP 'https://[^)]+\.woff2' | head -1 | xargs -I {} curl -o "$FONTS_DIR/spline-sans-$weight.woff2" {}
done

echo "Downloading Noto Sans fonts..."

for weight in "${weights[@]}"; do
    echo "Downloading Noto Sans $weight..."
    curl -s "https://fonts.googleapis.com/css2?family=Noto+Sans:wght@$weight&display=swap" \
        -H "User-Agent: Mozilla/5.0" | grep -oP 'https://[^)]+\.woff2' | head -1 | xargs -I {} curl -o "$FONTS_DIR/noto-sans-$weight.woff2" {}
done

echo "Downloading Material Symbols Outlined..."
curl -s "https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20..48,100..700,0..1,-50..200&display=swap" \
    -H "User-Agent: Mozilla/5.0" | grep -oP 'https://[^)]+\.woff2' | head -1 | xargs -I {} curl -o "$FONTS_DIR/material-symbols-outlined.woff2" {}

echo "Font download complete!"
echo "Downloaded files:"
ls -lh "$FONTS_DIR"/*.woff2 2>/dev/null || echo "No woff2 files found"
