# Ping 模块整改 - 测试指南

## 测试目标

验证新的探测策略是否能够：
1. 正确识别ICMP可达的IP
2. 正确处理ISP拦截的IP
3. 正确排序不同探测方法的IP
4. 提高排序后第一个IP的成功率

## 单元测试

### 测试1：ICMP优先级
**目标**：验证ICMP成功的IP排序靠前

```go
func TestICMPPriority(t *testing.T) {
    // 创建两个IP
    // IP A: ICMP成功，RTT=50ms
    // IP B: TCP成功，RTT=50ms
    
    // 预期：IP A排在IP B前面
    // 因为ICMP权重(0) < TCP权重(100)
}
```

### 测试2：UDP惩罚
**目标**：验证UDP成功的IP排序靠后

```go
func TestUDPPenalty(t *testing.T) {
    // 创建两个IP
    // IP A: TCP成功，RTT=50ms
    // IP B: UDP成功，RTT=50ms
    
    // 预期：IP A排在IP B前面
    // 因为TCP权重(100) < UDP权重(500)
}
```

### 测试3：ISP拦截场景
**目标**：验证ICMP不通的IP排序靠后

```go
func TestISPBlockingScenario(t *testing.T) {
    // 创建两个IP
    // IP A: ICMP成功，RTT=50ms
    // IP B: ICMP不通，UDP成功，RTT=50ms
    
    // 预期：IP A排在IP B前面
    // 因为ICMP权重(0) < UDP权重(500)
}
```

### 测试4：丢包惩罚
**目标**：验证高丢包IP排序靠后

```go
func TestPacketLossPenalty(t *testing.T) {
    // 创建两个IP
    // IP A: RTT=50ms, Loss=0%
    // IP B: RTT=50ms, Loss=30%
    
    // 预期：IP A排在IP B前面
    // 因为IP A得分 = 50 + 0*30 = 50
    //    IP B得分 = 50 + 30*30 = 950
}
```

### 测试5：探测方法标记
**目标**：验证探测方法被正确标记

```go
func TestProbeMethodMarking(t *testing.T) {
    // 测试各种IP的探测方法标记
    // ICMP成功 → ProbeMethod = "icmp"
    // TCP成功 → ProbeMethod = "tls"
    // UDP成功 → ProbeMethod = "udp53"
    // 完全失败 → ProbeMethod = "none"
}
```

### 测试6：权重计算
**目标**：验证综合得分计算正确

```go
func TestScoreCalculation(t *testing.T) {
    // 验证综合得分公式
    // 综合得分 = RTT + Loss*30 + 探测方法权重 + IP失效权重
    
    // 示例：
    // IP A: RTT=50, Loss=0%, ProbeMethod="icmp", FailureWeight=0
    // 得分 = 50 + 0*30 + 0 + 0 = 50
    
    // IP B: RTT=50, Loss=0%, ProbeMethod="udp53", FailureWeight=0
    // 得分 = 50 + 0*30 + 500 + 0 = 550
}
```

## 集成测试

### 测试场景1：完整的排序流程
**目标**：验证完整的ping和排序流程

```go
func TestCompleteSort(t *testing.T) {
    ips := []string{
        "1.2.3.4",      // ICMP成功
        "5.6.7.8",      // TCP成功
        "9.10.11.12",   // UDP成功
        "13.14.15.16",  // 完全失败
    }
    
    results := pinger.PingAndSort(ctx, ips, "example.com")
    
    // 验证排序顺序
    // 预期：1.2.3.4 > 5.6.7.8 > 9.10.11.12 > 13.14.15.16
}
```

### 测试场景2：ISP拦截识别
**目标**：验证能正确识别ISP拦截的IP

```go
func TestISPBlockingDetection(t *testing.T) {
    // 使用已知的ISP拦截IP进行测试
    // 例如：某些163.com的节点
    
    // 预期：
    // - ICMP不通
    // - TCP 443不通
    // - UDP DNS成功
    // - 排序靠后（因为UDP权重高）
}
```

### 测试场景3：缓存处理
**目标**：验证缓存的IP被正确标记

```go
func TestCacheHandling(t *testing.T) {
    // 第一次ping，缓存结果
    results1 := pinger.PingAndSort(ctx, ips, "example.com")
    
    // 第二次ping，应该从缓存获取
    results2 := pinger.PingAndSort(ctx, ips, "example.com")
    
    // 验证缓存的IP被标记为"cached"
    for _, r := range results2 {
        if r.ProbeMethod == "cached" {
            // 这是从缓存获取的
        }
    }
}
```

## 性能测试

### 测试1：ICMP ping性能
**目标**：验证ICMP ping不会显著增加延迟

```go
func BenchmarkICMPPing(b *testing.B) {
    for i := 0; i < b.N; i++ {
        p.icmpPing("8.8.8.8")
    }
}
```

### 测试2：完整ping流程性能
**目标**：验证新的探测顺序不会显著增加延迟

```go
func BenchmarkSmartPing(b *testing.B) {
    for i := 0; i < b.N; i++ {
        p.smartPing(ctx, "8.8.8.8", "example.com")
    }
}
```

## 监控指标

### 指标1：探测方法分布
**说明**：排序后第一个IP的探测方法分布

```
ICMP成功的比例：应该 > 80%
TCP成功的比例：应该 < 15%
UDP成功的比例：应该 < 5%
```

**监控方法**：
```go
// 统计探测方法
icmpCount := 0
tcpCount := 0
udpCount := 0

for _, r := range results {
    switch r.ProbeMethod {
    case "icmp":
        icmpCount++
    case "tls":
        tcpCount++
    case "udp53":
        udpCount++
    }
}

icmpRatio := float64(icmpCount) / float64(len(results))
tcpRatio := float64(tcpCount) / float64(len(results))
udpRatio := float64(udpCount) / float64(len(results))
```

### 指标2：排序后第一个IP的成功率
**说明**：排序后第一个IP的实际成功率

```
预期：> 95%
```

**监控方法**：
```go
// 记录排序后第一个IP的使用情况
firstIP := results[0].IP
successCount := 0
totalCount := 0

// 在实际使用中统计
if querySuccess {
    successCount++
}
totalCount++

successRate := float64(successCount) / float64(totalCount)
```

### 指标3：DNS查询重试率
**说明**：DNS查询需要重试的比例

```
预期：< 2%
```

**监控方法**：
```go
// 记录DNS查询重试情况
retryCount := 0
totalCount := 0

// 在DNS查询中统计
if needsRetry {
    retryCount++
}
totalCount++

retryRate := float64(retryCount) / float64(totalCount)
```

### 指标4：各探测方法的使用频率
**说明**：各探测方法被使用的频率

```
ICMP使用频率：应该最高
TCP使用频率：应该次高
UDP使用频率：应该最低
```

**监控方法**：
```go
// 统计各探测方法的使用频率
methodCount := make(map[string]int)

for _, r := range results {
    methodCount[r.ProbeMethod]++
}

for method, count := range methodCount {
    frequency := float64(count) / float64(len(results))
    log.Printf("%s: %.2f%%", method, frequency*100)
}
```

## 灰度发布测试

### 第一阶段：小范围测试（1天）
**范围**：开发环境
**测试内容**：
- 基本功能测试
- 单元测试
- 集成测试
- 性能测试

**验收标准**：
- ✅ 所有单元测试通过
- ✅ 所有集成测试通过
- ✅ 性能无显著下降
- ✅ 没有新的错误

### 第二阶段：灰度发布（3-5天）
**范围**：10%的用户
**监控指标**：
- 排序后第一个IP的成功率
- DNS查询重试率
- 各探测方法的使用频率
- 错误率

**验收标准**：
- ✅ 排序后第一个IP的成功率 > 90%
- ✅ DNS查询重试率 < 3%
- ✅ 错误率无显著增加
- ✅ 用户反馈正面

### 第三阶段：全量发布（1-2天）
**范围**：100%的用户
**监控指标**：
- 排序后第一个IP的成功率
- DNS查询重试率
- 各探测方法的使用频率
- 错误率

**验收标准**：
- ✅ 排序后第一个IP的成功率 > 95%
- ✅ DNS查询重试率 < 2%
- ✅ 错误率无显著增加
- ✅ 用户反馈正面

## 回滚方案

### 快速回滚
如果发现问题，可以快速回滚到之前的版本：

```bash
# 回滚到之前的版本
git revert <commit-hash>

# 或者直接恢复文件
git checkout HEAD~1 -- ping/
```

### 部分回滚
如果只想回滚第二阶段的改动，保留第一阶段的改动：

```bash
# 只回滚ICMP相关的改动
git checkout HEAD~1 -- ping/ping_probe.go
git checkout HEAD~1 -- ping/ping_test_methods.go
```

## 测试清单

- [ ] 单元测试：ICMP优先级
- [ ] 单元测试：UDP惩罚
- [ ] 单元测试：ISP拦截场景
- [ ] 单元测试：丢包惩罚
- [ ] 单元测试：探测方法标记
- [ ] 单元测试：权重计算
- [ ] 集成测试：完整排序流程
- [ ] 集成测试：ISP拦截识别
- [ ] 集成测试：缓存处理
- [ ] 性能测试：ICMP ping
- [ ] 性能测试：完整ping流程
- [ ] 灰度测试：第一阶段
- [ ] 灰度测试：第二阶段
- [ ] 灰度测试：第三阶段
- [ ] 监控指标：探测方法分布
- [ ] 监控指标：排序后第一个IP的成功率
- [ ] 监控指标：DNS查询重试率
- [ ] 监控指标：各探测方法的使用频率

