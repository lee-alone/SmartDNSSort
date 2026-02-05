@echo off
REM Download Google Fonts locally for offline use

setlocal enabledelayedexpansion

REM Get the absolute path to fonts directory
set SCRIPT_DIR=%~dp0
set WEB_DIR=%SCRIPT_DIR%..
set FONTS_DIR=%WEB_DIR%\fonts

if not exist "%FONTS_DIR%" mkdir "%FONTS_DIR%"

echo Downloading Spline Sans fonts...

REM Spline Sans weights
for %%W in (300 400 500 600 700) do (
    echo Downloading Spline Sans %%W...
    powershell -Command "^
        $css = Invoke-WebRequest -Uri 'https://fonts.googleapis.com/css2?family=Spline+Sans:wght@%%W^&display=swap' -Headers @{'User-Agent'='Mozilla/5.0'} -UseBasicParsing; ^
        $url = [regex]::Match($css.Content, 'https://[^)]+\.woff2').Value; ^
        if ($url) { Invoke-WebRequest -Uri $url -OutFile '%FONTS_DIR%\spline-sans-%%W.woff2' }^
    "
)

echo Downloading Noto Sans fonts...

for %%W in (300 400 500 600 700) do (
    echo Downloading Noto Sans %%W...
    powershell -Command "^
        $css = Invoke-WebRequest -Uri 'https://fonts.googleapis.com/css2?family=Noto+Sans:wght@%%W^&display=swap' -Headers @{'User-Agent'='Mozilla/5.0'} -UseBasicParsing; ^
        $url = [regex]::Match($css.Content, 'https://[^)]+\.woff2').Value; ^
        if ($url) { Invoke-WebRequest -Uri $url -OutFile '%FONTS_DIR%\noto-sans-%%W.woff2' }^
    "
)

echo Downloading Material Symbols Outlined...
powershell -Command "^
    $css = Invoke-WebRequest -Uri 'https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20..48,100..700,0..1,-50..200^&display=swap' -Headers @{'User-Agent'='Mozilla/5.0'} -UseBasicParsing; ^
    $url = [regex]::Match($css.Content, 'https://[^)]+\.woff2').Value; ^
    if ($url) { Invoke-WebRequest -Uri $url -OutFile '%FONTS_DIR%\material-symbols-outlined.woff2' }^
"

echo Font download complete!
echo Downloaded files:
dir "%FONTS_DIR%\*.woff2" 2>nul || echo No woff2 files found

pause
