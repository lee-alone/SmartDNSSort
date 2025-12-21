@echo off
chcp 65001 >nul
echo ========================================
echo 负响应 SOA 记录完整测试
echo ========================================
echo.

echo [测试 1] NXDOMAIN - 域名不存在
echo 查询不存在的域名...
dig @127.0.0.1 nonexistent-domain-test.com A +noall +authority
echo.
echo.

echo [测试 2] NODATA - 域名存在但无此类型记录
echo 查询 NODATA 响应...
dig @127.0.0.1 google.com AAAA +noall +authority
echo.
echo.

echo [测试 3] AdBlock NXDOMAIN 模式
echo 注意：需要在配置中设置 block_mode: nxdomain
echo 查询广告域名（如果有配置规则）...
dig @127.0.0.1 ads.example.com A +noall +authority
echo.
echo.

echo [测试 4] AdBlock REFUSED 模式
echo 注意：需要在配置中设置 block_mode: refuse
echo 如果使用 refuse 模式，应该看到 REFUSED 状态和 SOA 记录
echo （跳过此测试，需要修改配置）
echo.
echo.

echo [测试 5] SERVFAIL - 上游查询失败
echo 注意：这个测试需要模拟上游失败，可能看不到效果
echo 可以尝试查询一个会导致上游超时的域名
echo （跳过此测试，难以模拟）
echo.
echo.

echo [测试 6] 缓存测试 - 验证 TTL 递减
set RANDOM_DOMAIN=soa-test-%RANDOM%.invalid
echo 第一次查询: %RANDOM_DOMAIN%
dig @127.0.0.1 %RANDOM_DOMAIN% A +noall +authority
echo.

echo 等待 3 秒...
timeout /t 3 /nobreak >nul
echo.

echo 第二次查询（3秒后）: %RANDOM_DOMAIN%
echo TTL 应该递减约 3 秒...
dig @127.0.0.1 %RANDOM_DOMAIN% A +noall +authority
echo.
echo.

echo ========================================
echo 测试完成！
echo ========================================
echo.

echo 检查要点:
echo 1. 所有负响应都应该有 AUTHORITY SECTION
echo 2. SOA 记录格式: ^<domain^> ^<TTL^> IN SOA ns.smartdnssort.local. ...
echo 3. NXDOMAIN/NODATA 的 TTL 应该是 300 秒（negative_ttl_seconds）
echo 4. SERVFAIL 的 TTL 应该是 30 秒（error_cache_ttl_seconds）
echo 5. AdBlock 的 TTL 应该是 blocked_ttl 配置值（默认 3600）
echo 6. 缓存命中时 TTL 应该递减
echo.

pause
