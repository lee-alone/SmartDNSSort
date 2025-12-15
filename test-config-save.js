#!/usr/bin/env node

/**
 * 配置保存测试脚本
 * 模拟前端表单提交，测试后端是否正确保存配置
 */

const http = require('http');

// 测试数据：模拟从Web UI发来的配置
const testConfig = {
    dns: {
        listen_port: 5353,  // 改为与默认值不同的端口以验证改动
        enable_tcp: true,
        enable_ipv6: true,
    },
    upstream: {
        servers: [
            "192.168.1.10",
            "https://doh.pub/dns-query",
            "https://dns.google/dns-query"
        ],
        bootstrap_dns: [
            "192.168.1.11",
            "8.8.8.8:53"
        ],
        strategy: "sequential",
        timeout_ms: 5000,
        concurrency: 3,
        sequential_timeout: 300,
        racing_delay: 100,
        racing_max_concurrent: 2,
        nxdomain_for_errors: true,
        health_check: {
            enabled: true,
            failure_threshold: 3,
            circuit_breaker_threshold: 5,
            circuit_breaker_timeout: 30,
            success_threshold: 2,
        }
    },
    ping: {
        enabled: true,
        count: 3,
        timeout_ms: 1000,
        concurrency: 16,
        strategy: "min",
        max_test_ips: 0,
        rtt_cache_ttl_seconds: 300,
        enable_http_fallback: false,
    },
    cache: {
        fast_response_ttl: 20,  // 改为不同值
        user_return_ttl: 700,   // 改为不同值
        min_ttl_seconds: 3600,
        max_ttl_seconds: 84600,
        negative_ttl_seconds: 300,
        error_cache_ttl_seconds: 30,
        max_memory_mb: 128,
        eviction_threshold: 0.9,
        eviction_batch_percent: 0.1,
        keep_expired_entries: true,
        protect_prefetch_domains: true,
        save_to_disk_interval_minutes: 60,
    },
    prefetch: {
        enabled: false,
    },
    webui: {
        enabled: true,
        listen_port: 8080,
    },
    system: {
        max_cpu_cores: 0,
        sort_queue_workers: 4,
        refresh_workers: 4,
    }
};

console.log('测试配置保存功能\n');
console.log('发送配置数据到 http://localhost:8080/api/config');
console.log('修改项：');
console.log('  - DNS 端口: 53 → 5353');
console.log('  - Cache Fast Response TTL: 15 → 20');
console.log('  - Cache User Return TTL: 600 → 700');
console.log('\n');

const postData = JSON.stringify(testConfig);

const options = {
    hostname: 'localhost',
    port: 8080,
    path: '/api/config',
    method: 'POST',
    headers: {
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(postData)
    }
};

const req = http.request(options, (res) => {
    let data = '';

    res.on('data', (chunk) => {
        data += chunk;
    });

    res.on('end', () => {
        console.log(`\n✓ 服务器响应状态: ${res.statusCode}`);
        console.log('响应内容:', data);
        
        if (res.statusCode === 200) {
            console.log('\n✓ 配置已保存！');
            console.log('请检查 config.yaml 文件：');
            console.log('  - dns.listen_port 应该是 5353');
            console.log('  - cache.fast_response_ttl 应该是 20');
            console.log('  - cache.user_return_ttl 应该是 700');
        } else {
            console.log('\n✗ 配置保存失败！');
        }
    });
});

req.on('error', (e) => {
    console.error(`✗ 请求失败: ${e.message}`);
    console.log('确保 SmartDNSSort 服务正在运行且 WebUI 已启用');
});

req.write(postData);
req.end();
