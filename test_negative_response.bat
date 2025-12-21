@echo off
chcp 65001 >nul
echo ========================================
echo 负响应 SOA 记录测试
echo ========================================
echo.

echo [测试 1] NODATA 响应测试
echo 查询不存在的域名...
dig @127.0.0.1 this-domain-does-not-exist-12345.com A +noall +answer +authority +stats
echo.
echo.

echo [测试 2] SOA 记录详细信息
echo 使用 multiline 格式查看...
dig @127.0.0.1 nonexistent-test-domain.example A +multiline +noall +authority
echo.
echo.

echo [测试 3] 缓存测试 - 第一次查询
set RANDOM_DOMAIN=cache-test-%RANDOM%.invalid
echo 查询域名: %RANDOM_DOMAIN%
dig @127.0.0.1 %RANDOM_DOMAIN% A +noall +authority
echo.

echo 等待 5 秒...
timeout /t 5 /nobreak >nul
echo.

echo [测试 3] 缓存测试 - 5秒后再次查询
echo 查询同一域名，TTL 应该递减约 5 秒...
dig @127.0.0.1 %RANDOM_DOMAIN% A +noall +authority
echo.
echo.

echo [测试 4] 不同记录类型测试
echo 查询 AAAA 记录...
dig @127.0.0.1 fake-domain-xyz.test AAAA +noall +authority
echo.
echo.

echo ========================================
echo 测试完成！
echo ========================================
echo.

echo 检查要点:
echo 1. AUTHORITY SECTION 应该包含 SOA 记录
echo 2. SOA 记录格式: ^<domain^> ^<TTL^> IN SOA ns.smartdnssort.local. admin.smartdnssort.local. ...
echo 3. TTL 应该是 300 秒（negative_ttl_seconds 配置值）
echo 4. 缓存命中时，TTL 应该递减
echo.

pause
