# Download Google Fonts locally
# This script downloads Spline Sans and Noto Sans fonts from Google Fonts

$fontsDir = ".\fonts"
if (-not (Test-Path $fontsDir)) {
    New-Item -ItemType Directory -Path $fontsDir | Out-Null
}

Write-Host "Downloading fonts..."

# Spline Sans weights: 300, 400, 500, 600, 700
$splineSansWeights = @(300, 400, 500, 600, 700)
foreach ($weight in $splineSansWeights) {
    $url = "https://fonts.gstatic.com/s/splinesans/v8/px9_Hs7_-eWzVurKFmVU3ZxC2MH6pQ.woff2"
    # Note: Google Fonts uses a complex URL structure. We'll use a simpler approach.
}

# Alternative: Use Google Fonts CSS API to get the actual URLs
$cssUrl = "https://fonts.googleapis.com/css2?family=Spline+Sans:wght@300;400;500;600;700&display=swap"
$notoSansCssUrl = "https://fonts.googleapis.com/css2?family=Noto+Sans:wght@300;400;500;600;700&display=swap"

Write-Host "Fetching font URLs from Google Fonts API..."

# Get Spline Sans CSS
$splineCss = Invoke-WebRequest -Uri $cssUrl -Headers @{"User-Agent"="Mozilla/5.0"} -UseBasicParsing
$splineCss.Content | Out-File -FilePath "$fontsDir\spline-sans.css" -Encoding UTF8

# Get Noto Sans CSS
$notoCss = Invoke-WebRequest -Uri $notoSansCssUrl -Headers @{"User-Agent"="Mozilla/5.0"} -UseBasicParsing
$notoCss.Content | Out-File -FilePath "$fontsDir\noto-sans.css" -Encoding UTF8

Write-Host "Font CSS files downloaded to $fontsDir"
Write-Host "Next step: Extract font URLs from CSS and download .woff2 files"
