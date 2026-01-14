# 新的探测策略 - ICMP优先级方案

## 核心想法

基于你的观察，提出新的探测逻辑：

```
1. ICMP ping（最直接，最能代表IP可达性）
2. TCP ping（代表TCP连接可用）
3. UDP ping（备选方案，容易假阳性）
```

## 当前逻辑的问题

### 现状
```
TCP 443 → TLS握手 → UDP DNS → TCP 80
```

### 问题
1. **ICMP被忽视** - 没有ICMP探测
2. **UDP太激进** - TCP失败后直接尝试UDP
3. **无法识别ISP拦截** - ISP拦截TCP但允许UDP时，被认为IP可用
4. **假阳性太多** - UDP DNS成功不代表IP真正可用

## 新的探测策略

### 策略1：ICMP优先（推荐）

```go
func (p *Pinger) smartPing(ctx context.Context, ip, domain string) int {
    // 第1步：ICMP ping（最直接）
    if rtt := p.icmpPing(ip); rtt >= 0 {
        return rtt  // ICMP成功，直接返回，无惩罚
    }
    
    // 第2步：TCP 443（代表TCP连接）
    if rtt := p.tcpPingPort(ctx, ip, "443"); rtt >= 0 {
        // 第2.1步：TLS握手验证
        if rtt2 := p.tlsHandshakeWithSNI(ip, domain); rtt2 >= 0 {
            return rtt2 + 100  // TCP成功，增加100ms惩罚（相比ICMP）
        }
        // TLS失败直接判死刑
        return -1
    }
    
    // 第3步：UDP DNS（备选方案，增加大惩罚）
    if rtt := p.udpDnsPing(ip); rtt >= 0 {
        return rtt + 500  // UDP成功，增加500ms惩罚
    }
    
    // 第4步（可选）：TCP 80
    if p.enableHttpFallback {
        if rtt := p.tcpPingPort(ctx, ip, "80"); rtt >= 0 {
            return rtt + 300  // HTTP增加300ms惩罚
        }
    }
    
    return -1
}

// 新增：ICMP ping实现
func (p *Pinger) icmpPing(ip string) int {
    // 使用go-ping库实现ICMP ping
    // 返回RTT或-1
    // 
    // 伪代码：
    // pinger, err := ping.NewPinger(ip)
    // if err != nil {
    //     return -1
    // }
    // pinger.Count = 1
    // pinger.Timeout = time.Duration(p.timeoutMs) * time.Millisecond
    // err = pinger.Run()
    // if err != nil {
    //     return -1
    // }
    // stats := pinger.Statistics()
    // if stats.PacketsRecv > 0 {
    //     return int(stats.AvgRtt.Milliseconds())
    // }
    // return -1
}
```

### 策略2：带探测方法标记（可选）

```go
type Result struct {
    IP          string
    RTT         int
    Loss        float64
    ProbeMethod string  // "icmp", "tcp443", "tls", "udp53", "tcp80"
}

func (p *Pinger) pingIP(ctx context.Context, ip, domain string) *Result {
    var totalRTT int64 = 0
    successCount := 0
    probeMethod := ""
    
    for i := 0; i < p.count; i++ {
        rtt, method := p.smartPingWithMethod(ctx, ip, domain)
        if rtt >= 0 {
            totalRTT += int64(rtt)
            successCount++
            if probeMethod == "" {
                probeMethod = method
            }
        }
    }
    
    if successCount == 0 {
        return &Result{
            IP:          ip,
            RTT:         999999,
            Loss:        100,
            ProbeMethod: "none",
        }
    }
    
    avgRTT := int(totalRTT / int64(successCount))
    penalty := (p.count - successCount) * 150
    finalRTT := avgRTT + penalty
    
    return &Result{
        IP:          ip,
        RTT:         finalRTT,
        Loss:        float64(p.count-successCount) / float64(p.count) * 100,
        ProbeMethod: probeMethod,
    }
}

func (p *Pinger) smartPingWithMethod(ctx context.Context, ip, domain string) (int, string) {
    // ICMP
    if rtt := p.icmpPing(ip); rtt >= 0 {
        return rtt, "icmp"
    }
    
    // TCP 443
    if rtt := p.tcpPingPort(ctx, ip, "443"); rtt >= 0 {
        if rtt2 := p.tlsHandshakeWithSNI(ip, domain); rtt2 >= 0 {
            return rtt2, "tls"
        }
        return -1, ""
    }
    
    // UDP DNS
    if rtt := p.udpDnsPing(ip); rtt >= 0 {
        return rtt, "udp53"
    }
    
    // TCP 80
    if p.enableHttpFallback {
        if rtt := p.tcpPingPort(ctx, ip, "80"); rtt >= 0 {
            return rtt, "tcp80"
        }
    }
    
    return -1, ""
}

// 在排序时根据探测方法调整权重
func (p *Pinger) sortResults(results []Result) {
    sort.Slice(results, func(i, j int) bool {
        scoreI := results[i].RTT + int(results[i].Loss*30)
        scoreJ := results[j].RTT + int(results[j].Loss*30)
        
        // 根据探测方法调整权重
        scoreI += p.getProbeMethodPenalty(results[i].ProbeMethod)
        scoreJ += p.getProbeMethodPenalty(results[j].ProbeMethod)
        
        if scoreI != scoreJ {
            return scoreI < scoreJ
        }
        return results[i].IP < results[j].IP
    })
}

func (p *Pinger) getProbeMethodPenalty(method string) int {
    switch method {
    case "icmp":
        return 0      // 无惩罚，最优
    case "tls", "tcp443":
        return 100    // TCP增加100ms
    case "tcp80":
        return 300    // HTTP增加300ms
    case "udp53":
        return 500    // UDP增加500ms
    case "none":
        return 999999 // 完全失败
    default:
        return 0
    }
}
```

## 权重分配说明

### 为什么这样分配权重？

| 探测方法 | 惩罚 | 原因 |
|---------|------|------|
| ICMP | 0ms | 最直接，最能代表IP可达性 |
| TCP 443 + TLS | 100ms | 代表TCP连接可用，次优 |
| TCP 80 | 300ms | HTTP备选，可靠性较低 |
| UDP DNS | 500ms | 只代表DNS可用，容易假阳性 |

### 权重的含义

```
ICMP成功 → 得分 = RTT + Loss*30 + 0
TCP成功 → 得分 = RTT + Loss*30 + 100
UDP成功 → 得分 = RTT + Loss*30 + 500

例子：
IP A: ICMP成功，RTT=50ms, Loss=0% → 得分 = 50 + 0 + 0 = 50
IP B: TCP成功，RTT=50ms, Loss=0% → 得分 = 50 + 0 + 100 = 150
IP C: UDP成功，RTT=50ms, Loss=0% → 得分 = 50 + 0 + 500 = 550

结果：IP A排在第一位 ✓
```

## 实施步骤

### 第一阶段：快速修复（1-2小时）

1. **对UDP增加500ms惩罚**
   - 文件：`ping_probe.go`
   - 改动：1行代码
   - 效果：立即解决UDP假阳性问题

2. **删除RTT上限5000ms**
   - 文件：`ping_test_methods.go`
   - 改动：删除1行代码
   - 效果：高丢包IP排序靠后

### 第二阶段：添加ICMP探测（2-4小时）

1. **添加ICMP ping实现**
   - 文件：`ping_probe.go`
   - 改动：新增1个函数
   - 依赖：go-ping库

2. **修改smartPing逻辑**
   - 文件：`ping_probe.go`
   - 改动：修改smartPing函数
   - 效果：ICMP优先级最高

3. **增加丢包权重**
   - 文件：`ping_concurrent.go`
   - 改动：权重从18改为30
   - 效果：进一步惩罚不稳定IP

### 第三阶段：标记探测方法（可选）

1. **添加ProbeMethod字段**
   - 文件：`ping.go`
   - 改动：修改Result结构体
   - 效果：便于调试和监控

2. **实现getProbeMethodPenalty**
   - 文件：`ping_concurrent.go`
   - 改动：新增1个函数
   - 效果：根据探测方法调整权重

## 代码改动清单

### 第一阶段改动

**文件1：`ping/ping_probe.go`**
```go
// 修改smartPing函数
// 在UDP DNS部分增加惩罚
if rtt := p.udpDnsPing(ip); rtt >= 0 {
    return rtt + 500  // 增加500ms惩罚
}
```

**文件2：`ping/ping_test_methods.go`**
```go
// 删除RTT上限
// 删除这3行：
// if finalRTT > 5000 {
//     finalRTT = 5000
// }
```

### 第二阶段改动

**文件1：`ping/ping_probe.go`**
```go
// 新增ICMP ping函数
func (p *Pinger) icmpPing(ip string) int {
    // 实现ICMP ping
}

// 修改smartPing函数
func (p *Pinger) smartPing(ctx context.Context, ip, domain string) int {
    // 第1步：ICMP ping
    if rtt := p.icmpPing(ip); rtt >= 0 {
        return rtt
    }
    
    // 第2步：TCP 443
    if rtt := p.tcpPingPort(ctx, ip, "443"); rtt >= 0 {
        if rtt2 := p.tlsHandshakeWithSNI(ip, domain); rtt2 >= 0 {
            return rtt2 + 100  // 增加100ms惩罚
        }
        return -1
    }
    
    // 第3步：UDP DNS
    if rtt := p.udpDnsPing(ip); rtt >= 0 {
        return rtt + 500  // 增加500ms惩罚
    }
    
    // 第4步：TCP 80
    if p.enableHttpFallback {
        if rtt := p.tcpPingPort(ctx, ip, "80"); rtt >= 0 {
            return rtt + 300  // 增加300ms惩罚
        }
    }
    
    return -1
}
```

**文件2：`ping/ping_concurrent.go`**
```go
// 修改sortResults函数
// 权重从18改为30
scoreI := results[i].RTT + int(results[i].Loss*30)
scoreJ := results[j].RTT + int(results[j].Loss*30)
```

## 测试验证

### 单元测试

```go
// 测试ICMP优先级
func TestICMPPriority(t *testing.T) {
    // 创建ICMP成功、TCP失败的IP
    // 验证其排序位置在TCP成功的IP之前
}

// 测试UDP惩罚
func TestUDPPenalty(t *testing.T) {
    // 创建UDP成功、TCP失败的IP
    // 验证其排序位置在TCP成功的IP之后
}

// 测试ISP拦截场景
func TestISPBlockingScenario(t *testing.T) {
    // 创建ICMP不通、UDP成功的IP
    // 验证其排序位置在最后
}
```

### 集成测试

```go
// 测试完整的排序流程
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

### 监控指标

```
- 排序后第一个IP的探测方法分布
  - ICMP成功的比例（应该>80%）
  - TCP成功的比例（应该<15%）
  - UDP成功的比例（应该<5%）

- 排序后第一个IP的实际成功率（应该>95%）

- 高丢包IP（>30%）的排序位置（应该在后50%）

- DNS查询重试率（应该<2%）
```

## 风险评估

### 第一阶段风险：低
- 只改动2个地方
- 改动最小
- 效果明显
- 容易回滚

### 第二阶段风险：中
- 需要添加ICMP探测
- 需要新增依赖（go-ping库）
- 需要更多测试
- 但改动仍然相对简单

### 第三阶段风险：低
- 只是添加标记和调整权重
- 不影响核心逻辑
- 便于调试和监控

## 灰度发布计划

### 第一阶段：小范围测试（1天）
- 在开发环境测试
- 验证基本功能
- 检查是否有回归

### 第二阶段：灰度发布（3-5天）
- 发布到10%的用户
- 监控关键指标
- 收集反馈

### 第三阶段：全量发布（1-2天）
- 发布到100%的用户
- 持续监控
- 准备回滚方案

## 总结

新的探测策略的优势：
1. **ICMP优先** - 最直接地测试IP可达性
2. **TCP次优** - 代表TCP连接可用
3. **UDP备选** - 只在TCP失败时尝试
4. **权重清晰** - 根据探测方法调整权重
5. **易于调试** - 标记探测方法便于监控

预期效果：
- 排序后第一个IP的成功率从~80%提高到>95%
- DNS查询重试率从~5%降低到<2%
- 用户体验明显改善
