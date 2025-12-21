# 负响应 SOA 记录测试脚本
# 使用方法: 重新编译程序后运行此脚本

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "负响应 SOA 记录测试" -ForegroundColor Cyan
Write-Host "========================================`n" -ForegroundColor Cyan

# 测试1: NODATA 响应（域名不存在或无记录）
Write-Host "[测试 1] NODATA 响应测试" -ForegroundColor Yellow
Write-Host "查询不存在的域名..." -ForegroundColor Gray
$result1 = dig @127.0.0.1 this-domain-does-not-exist-12345.com A +noall +answer +authority +stats
Write-Host $result1
Write-Host ""

# 测试2: 使用 multiline 格式查看 SOA 详情
Write-Host "[测试 2] SOA 记录详细信息" -ForegroundColor Yellow
Write-Host "使用 multiline 格式查看..." -ForegroundColor Gray
$result2 = dig @127.0.0.1 nonexistent-test-domain.example A +multiline +noall +authority
Write-Host $result2
Write-Host ""

# 测试3: 缓存命中测试（第一次查询）
Write-Host "[测试 3] 缓存测试 - 第一次查询" -ForegroundColor Yellow
$domain = "cache-test-$(Get-Random).invalid"
Write-Host "查询域名: $domain" -ForegroundColor Gray
$result3a = dig @127.0.0.1 $domain A +noall +authority
Write-Host $result3a
Write-Host ""

# 等待5秒
Write-Host "等待 5 秒..." -ForegroundColor Gray
Start-Sleep -Seconds 5

# 测试3: 缓存命中测试（第二次查询 - 应该看到 TTL 递减）
Write-Host "[测试 3] 缓存测试 - 5秒后再次查询" -ForegroundColor Yellow
Write-Host "查询同一域名，TTL 应该递减约 5 秒..." -ForegroundColor Gray
$result3b = dig @127.0.0.1 $domain A +noall +authority
Write-Host $result3b
Write-Host ""

# 测试4: 不同记录类型
Write-Host "[测试 4] 不同记录类型测试" -ForegroundColor Yellow
Write-Host "查询 AAAA 记录..." -ForegroundColor Gray
$result4 = dig @127.0.0.1 fake-domain-xyz.test AAAA +noall +authority
Write-Host $result4
Write-Host ""

# 总结
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "测试完成！" -ForegroundColor Cyan
Write-Host "========================================`n" -ForegroundColor Cyan

Write-Host "✅ 检查要点:" -ForegroundColor Green
Write-Host "1. AUTHORITY SECTION 应该包含 SOA 记录" -ForegroundColor White
Write-Host "2. SOA 记录格式: <domain> <TTL> IN SOA ns.smartdnssort.local. admin.smartdnssort.local. ..." -ForegroundColor White
Write-Host "3. TTL 应该是 300 秒（negative_ttl_seconds 配置值）" -ForegroundColor White
Write-Host "4. 缓存命中时，TTL 应该递减" -ForegroundColor White
Write-Host ""
