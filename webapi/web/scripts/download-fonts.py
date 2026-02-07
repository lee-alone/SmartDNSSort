#!/usr/bin/env python3
"""
Download Google Fonts locally for offline use.
This script fetches font files from Google Fonts and saves them locally.
"""

import re
import sys
from pathlib import Path
from urllib.request import urlopen, Request
from urllib.error import URLError

# Get the absolute path to the fonts directory
SCRIPT_DIR = Path(__file__).parent.absolute()
WEB_DIR = SCRIPT_DIR.parent
FONTS_DIR = WEB_DIR / "fonts"
FONTS_DIR.mkdir(exist_ok=True)

def get_font_urls(font_name, weights):
    """Fetch font URLs from Google Fonts CSS."""
    urls = []
    for weight in weights:
        url = f"https://fonts.googleapis.com/css2?family={font_name}:wght@{weight}&display=swap"
        try:
            req = Request(url, headers={"User-Agent": "Mozilla/5.0"})
            with urlopen(req, timeout=10) as response:
                css_content = response.read().decode('utf-8')
                # Extract font URLs (both woff2 and ttf)
                matches = re.findall(r'url\((https://[^\s)]+)\)', css_content)
                if matches:
                    urls.append((weight, matches[0]))
        except URLError as e:
            print(f"Error fetching {font_name} {weight}: {e}")
    return urls

def download_font(url, filename):
    """Download a font file."""
    file_path = FONTS_DIR / filename
    if file_path.exists():
        print(f"Skipping {filename} (already exists)")
        return True
        
    try:
        print(f"Downloading {filename}...", end=" ", flush=True)
        req = Request(url, headers={"User-Agent": "Mozilla/5.0"})
        with urlopen(req, timeout=30) as response:
            with open(file_path, 'wb') as f:
                f.write(response.read())
        print("✓")
        return True
    except URLError as e:
        print(f"✗ ({e})")
        return False
    except Exception as e:
        print(f"✗ ({e})")
        return False

def main():
    print("Downloading Google Fonts locally...\n")
    
    weights = [300, 400, 500, 600, 700]
    downloaded = 0
    skipped = 0
    failed = 0
    
    # Check if all files exist first to avoid even fetching CSS
    def check_all_exist(prefix, weights):
        for weight in weights:
            # We check for woff2, woff, and ttf
            exists = False
            for ext in ['woff2', 'woff', 'ttf']:
                if (FONTS_DIR / f"{prefix}-{weight}.{ext}").exists():
                    exists = True
                    break
            if not exists:
                return False
        return True

    # Download Spline Sans
    print("Spline Sans:")
    if check_all_exist("spline-sans", weights):
        print("  All Spline Sans fonts already exist.")
        skipped += len(weights)
    else:
        urls = get_font_urls("Spline+Sans", weights)
        for weight, url in urls:
            # Determine file extension
            ext = url.split('.')[-1].split('?')[0]
            filename = f"spline-sans-{weight}.{ext}"
            if download_font(url, filename):
                downloaded += 1
            else:
                failed += 1
    
    # Download Noto Sans
    print("\nNoto Sans:")
    if check_all_exist("noto-sans", weights):
        print("  All Noto Sans fonts already exist.")
        skipped += len(weights)
    else:
        urls = get_font_urls("Noto+Sans", weights)
        for weight, url in urls:
            ext = url.split('.')[-1].split('?')[0]
            filename = f"noto-sans-{weight}.{ext}"
            if download_font(url, filename):
                downloaded += 1
            else:
                failed += 1
    
    # Download Material Symbols
    print("\nMaterial Symbols Outlined:")
    material_exists = False
    for ext in ['woff2', 'woff', 'ttf']:
        if (FONTS_DIR / f"material-symbols-outlined.{ext}").exists():
            material_exists = True
            break
            
    if material_exists:
        print("  Material Symbols Outlined already exists.")
        skipped += 1
    else:
        url = f"https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20..48,100..700,0..1,-50..200&display=swap"
        try:
            req = Request(url, headers={"User-Agent": "Mozilla/5.0"})
            with urlopen(req, timeout=10) as response:
                css_content = response.read().decode('utf-8')
                matches = re.findall(r'url\((https://[^\s)]+)\)', css_content)
                if matches:
                    font_url = matches[0]
                    ext = font_url.split('.')[-1].split('?')[0]
                    filename = f"material-symbols-outlined.{ext}"
                    if download_font(font_url, filename):
                        downloaded += 1
                    else:
                        failed += 1
        except Exception as e:
            print(f"Error: {e}")
            failed += 1
    
    print(f"\n✓ Font processing complete!")
    if downloaded > 0:
        print(f"Downloaded: {downloaded} files")
    if skipped > 0:
        print(f"Skipped: {skipped} files (already exist)")
    if failed > 0:
        print(f"Failed: {failed} files")
    
    print(f"\nFiles in {FONTS_DIR}:")
    font_files = list(FONTS_DIR.glob("*")) 
    font_files = [f for f in font_files if f.suffix in ['.woff2', '.ttf', '.woff']]
    if font_files:
        for f in sorted(font_files):
            size = f.stat().st_size / 1024
            print(f"  {f.name} ({size:.1f} KB)")
    else:
        print("  (No font files found)")

if __name__ == "__main__":
    main()
