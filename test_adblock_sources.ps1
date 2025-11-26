# AdBlock 多规则源测试脚本 (PowerShell)

Write-Host "=== AdBlock 多规则源测试 ===" -ForegroundColor Cyan
Write-Host ""

$ApiBase = "http://localhost:8080/api"

# 1. 检查 AdBlock 状态
Write-Host "1. 检查 AdBlock 状态..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "$ApiBase/adblock/status" -Method Get
    $response | ConvertTo-Json -Depth 10
} catch {
    Write-Host "错误: $_" -ForegroundColor Red
}
Write-Host ""

# 2. 获取当前规则源列表
Write-Host "2. 获取当前规则源列表..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "$ApiBase/adblock/sources" -Method Get
    $response | ConvertTo-Json -Depth 10
} catch {
    Write-Host "错误: $_" -ForegroundColor Red
}
Write-Host ""

# 3. 添加测试规则源
Write-Host "3. 添加测试规则源..." -ForegroundColor Yellow

# 添加 EasyList
Write-Host "  添加 EasyList..." -ForegroundColor Gray
try {
    $body = @{
        url = "https://easylist.to/easylist/easylist.txt"
    } | ConvertTo-Json
    
    $response = Invoke-RestMethod -Uri "$ApiBase/adblock/sources" `
        -Method Post `
        -ContentType "application/json" `
        -Body $body
    $response | ConvertTo-Json -Depth 10
} catch {
    Write-Host "  错误: $_" -ForegroundColor Red
}

# 添加 EasyList China
Write-Host "  添加 EasyList China..." -ForegroundColor Gray
try {
    $body = @{
        url = "https://easylist-downloads.adblockplus.org/easylistchina.txt"
    } | ConvertTo-Json
    
    $response = Invoke-RestMethod -Uri "$ApiBase/adblock/sources" `
        -Method Post `
        -ContentType "application/json" `
        -Body $body
    $response | ConvertTo-Json -Depth 10
} catch {
    Write-Host "  错误: $_" -ForegroundColor Red
}
Write-Host ""

# 4. 再次获取规则源列表
Write-Host "4. 再次获取规则源列表（应该有2个源）..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "$ApiBase/adblock/sources" -Method Get
    Write-Host "  规则源数量: $($response.data.Count)" -ForegroundColor Green
    $response | ConvertTo-Json -Depth 10
} catch {
    Write-Host "错误: $_" -ForegroundColor Red
}
Write-Host ""

# 5. 触发规则更新
Write-Host "5. 触发规则更新..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "$ApiBase/adblock/update" -Method Post
    $response | ConvertTo-Json -Depth 10
} catch {
    Write-Host "错误: $_" -ForegroundColor Red
}
Write-Host ""

# 6. 等待更新
Write-Host "6. 等待10秒让更新完成..." -ForegroundColor Yellow
Start-Sleep -Seconds 10

# 7. 检查更新后的状态
Write-Host "7. 检查更新后的状态..." -ForegroundColor Yellow
try {
    $response = Invoke-RestMethod -Uri "$ApiBase/adblock/status" -Method Get
    Write-Host "  总规则数: $($response.data.total_rules)" -ForegroundColor Green
    Write-Host "  引擎: $($response.data.engine)" -ForegroundColor Green
    $response | ConvertTo-Json -Depth 10
} catch {
    Write-Host "错误: $_" -ForegroundColor Red
}
Write-Host ""

# 8. 测试域名拦截
Write-Host "8. 测试域名拦截..." -ForegroundColor Yellow

# 测试广告域名
Write-Host "  测试 doubleclick.net (应该被拦截)..." -ForegroundColor Gray
try {
    $body = @{
        domain = "doubleclick.net"
    } | ConvertTo-Json
    
    $response = Invoke-RestMethod -Uri "$ApiBase/adblock/test" `
        -Method Post `
        -ContentType "application/json" `
        -Body $body
    
    if ($response.data.blocked) {
        Write-Host "  ✓ 已拦截！规则: $($response.data.rule)" -ForegroundColor Green
    } else {
        Write-Host "  ✗ 未拦截" -ForegroundColor Red
    }
} catch {
    Write-Host "  错误: $_" -ForegroundColor Red
}

# 测试正常域名
Write-Host "  测试 google.com (不应该被拦截)..." -ForegroundColor Gray
try {
    $body = @{
        domain = "google.com"
    } | ConvertTo-Json
    
    $response = Invoke-RestMethod -Uri "$ApiBase/adblock/test" `
        -Method Post `
        -ContentType "application/json" `
        -Body $body
    
    if ($response.data.blocked) {
        Write-Host "  ✗ 被拦截了（不应该）" -ForegroundColor Red
    } else {
        Write-Host "  ✓ 未拦截（正确）" -ForegroundColor Green
    }
} catch {
    Write-Host "  错误: $_" -ForegroundColor Red
}

Write-Host ""
Write-Host "=== 测试完成 ===" -ForegroundColor Cyan

# 9. 检查缓存目录
Write-Host ""
Write-Host "9. 检查缓存目录..." -ForegroundColor Yellow
$cacheDir = ".\adblock_cache"
if (Test-Path $cacheDir) {
    Write-Host "  缓存目录存在: $cacheDir" -ForegroundColor Green
    Get-ChildItem $cacheDir | ForEach-Object {
        Write-Host "    - $($_.Name) ($([math]::Round($_.Length/1KB, 2)) KB)" -ForegroundColor Gray
    }
} else {
    Write-Host "  缓存目录不存在: $cacheDir" -ForegroundColor Red
}
