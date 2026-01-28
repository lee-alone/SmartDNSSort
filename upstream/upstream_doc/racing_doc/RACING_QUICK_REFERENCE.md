# Racing 策略 - 快速参考卡片

## 核心改进一览

### 1️⃣ 健康状态检查
```go
shouldSkipServerInRacing(srv) → bool
```
- ✅ 跳过 Unhealthy 服务器
- ✅ 保留 Degraded 服务器
- ✅ 启用 Healthy 服务器

### 2️⃣ 动态批次参数
```go
calculateRacingBatchParams(remainingCount, stdDev) → (batchSize, stagger)
```

| stdDev | 服务器数 | 批次 | 间隔 |
|--------|---------|------|------|
| <50ms  | ≤5      | 2    | 20ms |
| <50ms  | >5      | 3    | 20ms |
| >50ms  | ≤5      | 3    | 15ms |
| >50ms  | >5      | 4    | 15ms |

### 3️⃣ 错误分类
```go
isNetworkError(err) → bool
```

**网络错误** (触发抢跑):
- connection refused/reset
- timeout
- host unreachable

**应用错误** (不触发):
- SERVFAIL
- REFUSED
- DNS rcode 错误

---

## 关键函数速查

### 主查询函数
```go
func (u *Manager) queryRacing(ctx context.Context, domain string, qtype uint16, r *dns.Msg, dnssec bool) (*QueryResultWithTTL, error)
```

### 辅助函数
```go
// 错误分类
func isNetworkError(err error) bool

// 服务器过滤
func shouldSkipServerInRacing(srv *HealthAwareUpstream) bool

// 动态参数
func (u *Manager) calculateRacingBatchParams(remainingCount int, stdDev time.Duration) (int, time.Duration)

// 字符串匹配
func contains(s, substr string) bool
func toLower(b byte) byte
```

---

## 日志输出示例

### 正常流程
```
[queryRacing] 开始竞争查询: example.com (延迟=50ms, 标准差=25ms, 最大并发=4)
[queryRacing] 启动备选梯队: 批次大小=3, 间隔=15ms
[queryRacing] 竞速获胜者: secondary:53 (耗时: 45ms)
```

### 错误抢跑触发
```
[queryRacing] 主请求网络错误，触发错误抢跑: connection refused
[queryRacing] 启动备选梯队: 批次大小=3, 间隔=15ms
[queryRacing] 竞速获胜者: secondary:53 (耗时: 20ms)
```

### 服务器跳过
```
[queryRacing] 跳过不健康的服务器: tertiary:53 (状态=2)
```

---

## 性能指标

### 获取统计信息
```go
stats := u.GetDynamicParamStats()
// 返回:
// - avg_latency_ms: 平均延迟
// - racing_delay_ms: 竞速延迟
// - sequential_timeout_ms: 顺序超时
```

### 关键指标
- `racing_delay_ms`: 自适应竞速延迟（20-200ms）
- `stdDev`: 延迟标准差（用于动态调整）
- `batch_size`: 当前批次大小（2-4）
- `early_trigger_count`: 错误抢跑触发次数

---

## 配置参数

### Manager 初始化
```go
manager := &Manager{
    racingDelayMs:       100,  // 初始延迟（会被自适应覆盖）
    racingMaxConcurrent: 4,    // 最大并发数
    dynamicParamOptimization: &DynamicParamOptimization{
        ewmaAlpha:  0.2,       // EWMA 平滑因子
        maxStepMs:  10,        // 最大步长
        avgLatency: 200 * time.Millisecond,
    },
}
```

### 自适应参数范围
- 竞速延迟: 20ms - 200ms
- 批次大小: 2 - 4
- 间隔: 15ms - 20ms

---

## 测试命令

### 运行所有 Racing 测试
```bash
go test -v ./upstream -run Racing
```

### 运行特定测试
```bash
go test -v ./upstream -run TestIsNetworkError
go test -v ./upstream -run TestCalculateRacingBatchParams
go test -v ./upstream -run TestShouldSkipServerInRacing
```

### 运行集成测试
```bash
go test -v ./upstream -run TestRacingEarlyTrigger
```

---

## 常见场景处理

### 场景 1: 主服务器宕机
```
主服务器报错 (network error)
  ↓
立即触发错误抢跑 (close cancelDelayChan)
  ↓
立即启动备选梯队 (0ms 延迟)
  ↓
备选服务器快速响应
```

### 场景 2: 网络极度不稳定
```
高标准差 (>50ms)
  ↓
自适应延迟缩短到 20ms
  ↓
批次大小增加到 3-4
  ↓
更激进地启动备选
```

### 场景 3: 多个服务器，网络稳定
```
低标准差 (<50ms)
  ↓
自适应延迟保持 100ms+
  ↓
批次大小保持 2-3
  ↓
保守策略，资源利用高效
```

---

## 故障排查

### 问题: 错误抢跑触发过于频繁
**原因**: 网络不稳定或主服务器故障
**解决**: 检查主服务器健康状态，考虑调整 K 系数

### 问题: 竞速延迟过长
**原因**: 标准差计算不准确或样本不足
**解决**: 等待更多样本积累，检查 RecordQueryLatency 是否被调用

### 问题: 某些服务器被频繁跳过
**原因**: 服务器处于 Degraded 或 Unhealthy 状态
**解决**: 检查服务器健康检查配置，考虑调整阈值

---

## 性能优化建议

### 1. 调整 EWMA 因子
```go
ewmaAlpha: 0.2  // 默认，更重视最近的数据
ewmaAlpha: 0.1  // 更平滑，减少波动
ewmaAlpha: 0.3  // 更敏感，快速响应变化
```

### 2. 调整方差权重 (K 系数)
```go
const K = 0.5  // 默认
// K 越大，标准差对延迟的影响越大
// K 越小，标准差的影响越小
```

### 3. 调整批次大小范围
```go
// 在 calculateRacingBatchParams 中修改
batchSize = min(batchSize+1, 5)  // 最大改为 5
```

---

## 监控检查清单

- [ ] 竞速延迟是否在 20-200ms 范围内
- [ ] 错误抢跑触发频率是否合理
- [ ] 批次大小是否根据网络状况动态调整
- [ ] 平均延迟是否逐步优化
- [ ] 成功率是否保持在 95%+ 以上
- [ ] 日志输出是否清晰有用

---

## 相关文档

- 📖 `RACING_IMPROVEMENTS.md` - 详细的改进文档
- 📋 `RACING_REFACTOR_SUMMARY.md` - 重构总结
- 🧪 `manager_racing_test.go` - 测试用例

---

**最后更新**: 2026-01-28
**版本**: 1.0
**状态**: ✅ 生产就绪
