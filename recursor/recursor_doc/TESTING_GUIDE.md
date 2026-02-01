# 高优先级修复验证测试指南

## 测试目标

验证以下三个高优先级修复：
1. Goroutine 泄漏已修复
2. stopCh 复用问题已修复
3. 循环依赖问题已修复

---

## 测试 1: 多次启停循环测试

**目标：** 验证 Start/Stop 可以多次循环，不会 panic

**测试代码：**
```go
func TestMultipleStartStop(t *testing.T) {
    m := NewManager(5353)
    
    // 循环 5 次启停
    for i := 0; i < 5; i++ {
        t.Logf("Iteration %d: Starting...", i+1)
        if err := m.Start(); err != nil {
            t.Fatalf("Start failed on iteration %d: %v", i+1, err)
        }
        
        // 等待启动完成
        time.Sleep(2 * time.Second)
        
        if !m.IsEnabled() {
            t.Fatalf("Manager not enabled after Start on iteration %d", i+1)
        }
        
        t.Logf("Iteration %d: Stopping...", i+1)
        if err := m.Stop(); err != nil {
            t.Fatalf("Stop failed on iteration %d: %v", i+1, err)
        }
        
        if m.IsEnabled() {
            t.Fatalf("Manager still enabled after Stop on iteration %d", i+1)
        }
        
        // 等待清理完成
        time.Sleep(1 * time.Second)
    }
    
    t.Log("✓ Multiple Start/Stop cycles completed successfully")
}
```

**预期结果：**
- 所有 5 次循环都成功完成
- 没有 panic
- 没有 "send on closed channel" 错误

---

## 测试 2: Goroutine 泄漏检测

**目标：** 验证 Start/Stop 不会导致 goroutine 泄漏

**测试代码：**
```go
func TestGoroutineLeakDetection(t *testing.T) {
    initialGoroutines := runtime.NumGoroutine()
    t.Logf("Initial goroutines: %d", initialGoroutines)
    
    m := NewManager(5353)
    
    // 启动
    if err := m.Start(); err != nil {
        t.Fatalf("Start failed: %v", err)
    }
    
    time.Sleep(2 * time.Second)
    
    startedGoroutines := runtime.NumGoroutine()
    t.Logf("Goroutines after Start: %d (added: %d)", 
        startedGoroutines, startedGoroutines-initialGoroutines)
    
    // 停止
    if err := m.Stop(); err != nil {
        t.Fatalf("Stop failed: %v", err)
    }
    
    time.Sleep(2 * time.Second)
    
    // 强制 GC 以清理已完成的 goroutine
    runtime.GC()
    time.Sleep(1 * time.Second)
    
    finalGoroutines := runtime.NumGoroutine()
    t.Logf("Goroutines after Stop: %d", finalGoroutines)
    
    // 允许 +2 的容差（可能有其他后台任务）
    if finalGoroutines > initialGoroutines+2 {
        t.Errorf("Goroutine leak detected: %d -> %d (leaked: %d)",
            initialGoroutines, finalGoroutines, finalGoroutines-initialGoroutines)
    } else {
        t.Log("✓ No goroutine leak detected")
    }
}
```

**预期结果：**
- Stop 后 goroutine 数量回到接近初始值
- 没有明显的 goroutine 泄漏

---

## 测试 3: 进程重启测试

**目标：** 验证进程意外退出时能正确重启

**测试代码：**
```go
func TestProcessRestart(t *testing.T) {
    m := NewManager(5353)
    
    if err := m.Start(); err != nil {
        t.Fatalf("Start failed: %v", err)
    }
    
    time.Sleep(2 * time.Second)
    
    initialPID := m.cmd.Process.Pid
    t.Logf("Initial PID: %d", initialPID)
    
    // 模拟进程崩溃
    if err := m.cmd.Process.Kill(); err != nil {
        t.Fatalf("Failed to kill process: %v", err)
    }
    
    t.Log("Process killed, waiting for restart...")
    
    // 等待重启
    time.Sleep(5 * time.Second)
    
    if !m.IsEnabled() {
        t.Fatal("Manager disabled after process crash")
    }
    
    if m.cmd == nil || m.cmd.Process == nil {
        t.Fatal("Process not restarted")
    }
    
    newPID := m.cmd.Process.Pid
    t.Logf("New PID after restart: %d", newPID)
    
    if newPID == initialPID {
        t.Error("Process PID unchanged after restart")
    }
    
    restartAttempts := m.GetRestartAttempts()
    t.Logf("Restart attempts: %d", restartAttempts)
    
    if restartAttempts != 1 {
        t.Errorf("Expected 1 restart attempt, got %d", restartAttempts)
    }
    
    // 清理
    if err := m.Stop(); err != nil {
        t.Fatalf("Stop failed: %v", err)
    }
    
    t.Log("✓ Process restart test passed")
}
```

**预期结果：**
- 进程被杀死后自动重启
- 新的 PID 不同于旧的 PID
- 重启计数器正确更新

---

## 测试 4: 健康检查循环测试

**目标：** 验证健康检查循环正常工作

**测试代码：**
```go
func TestHealthCheckLoop(t *testing.T) {
    m := NewManager(5353)
    
    if err := m.Start(); err != nil {
        t.Fatalf("Start failed: %v", err)
    }
    
    time.Sleep(2 * time.Second)
    
    initialCheckTime := m.GetLastHealthCheck()
    t.Logf("Initial health check time: %v", initialCheckTime)
    
    // 等待健康检查周期（30 秒）
    // 为了加快测试，我们只等待一部分
    time.Sleep(35 * time.Second)
    
    finalCheckTime := m.GetLastHealthCheck()
    t.Logf("Final health check time: %v", finalCheckTime)
    
    if finalCheckTime.Equal(initialCheckTime) {
        t.Error("Health check time not updated")
    } else {
        t.Log("✓ Health check loop is working")
    }
    
    // 清理
    if err := m.Stop(); err != nil {
        t.Fatalf("Stop failed: %v", err)
    }
}
```

**预期结果：**
- 健康检查时间定期更新
- 没有错误日志

---

## 测试 5: 并发启停测试

**目标：** 验证并发调用 Start/Stop 不会导致竞态条件

**测试代码：**
```go
func TestConcurrentStartStop(t *testing.T) {
    m := NewManager(5353)
    
    var wg sync.WaitGroup
    errors := make(chan error, 10)
    
    // 5 个 goroutine 并发调用 Start
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            if err := m.Start(); err != nil {
                // 只有第一个应该成功，其他应该返回 "already running"
                if !strings.Contains(err.Error(), "already running") {
                    errors <- fmt.Errorf("goroutine %d: unexpected error: %v", id, err)
                }
            }
        }(i)
    }
    
    wg.Wait()
    
    time.Sleep(2 * time.Second)
    
    // 5 个 goroutine 并发调用 Stop
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            if err := m.Stop(); err != nil {
                errors <- fmt.Errorf("goroutine %d: Stop failed: %v", id, err)
            }
        }(i)
    }
    
    wg.Wait()
    
    close(errors)
    
    for err := range errors {
        t.Errorf("Concurrent test error: %v", err)
    }
    
    if len(errors) == 0 {
        t.Log("✓ Concurrent Start/Stop test passed")
    }
}
```

**预期结果：**
- 只有第一个 Start 成功
- 其他 Start 返回 "already running" 错误
- 所有 Stop 都成功
- 没有 panic 或竞态条件

---

## 手动测试清单

### Windows 测试
- [ ] 在 Windows 上运行 Start/Stop 循环 5 次
- [ ] 验证没有 panic
- [ ] 验证没有 "send on closed channel" 错误
- [ ] 检查任务管理器中的进程数量

### Linux 测试
- [ ] 在 Linux 上运行 Start/Stop 循环 5 次
- [ ] 验证没有 panic
- [ ] 使用 `ps aux | grep unbound` 检查进程
- [ ] 检查 systemctl 状态

### 跨平台测试
- [ ] 在 Windows 上测试嵌入式 unbound
- [ ] 在 Linux 上测试系统 unbound
- [ ] 验证超时时间正确（Windows 30s, Linux 20s）

---

## 性能基准测试

**目标：** 验证修复不会显著影响性能

**测试代码：**
```go
func BenchmarkStartStop(b *testing.B) {
    m := NewManager(5353)
    
    b.ResetTimer()
    
    for i := 0; i < b.N; i++ {
        if err := m.Start(); err != nil {
            b.Fatalf("Start failed: %v", err)
        }
        
        time.Sleep(1 * time.Second)
        
        if err := m.Stop(); err != nil {
            b.Fatalf("Stop failed: %v", err)
        }
    }
}
```

**预期结果：**
- 每次 Start/Stop 循环时间稳定
- 没有显著的性能下降

---

## 测试执行步骤

1. **编译测试**
   ```bash
   go test -v ./recursor -run TestMultipleStartStop
   ```

2. **运行所有测试**
   ```bash
   go test -v ./recursor
   ```

3. **运行基准测试**
   ```bash
   go test -bench=. -benchtime=10s ./recursor
   ```

4. **检查竞态条件**
   ```bash
   go test -race ./recursor
   ```

---

## 预期测试结果

所有测试应该通过，输出类似：
```
=== RUN   TestMultipleStartStop
    manager_test.go:XX: Iteration 1: Starting...
    manager_test.go:XX: Iteration 1: Stopping...
    ...
    manager_test.go:XX: ✓ Multiple Start/Stop cycles completed successfully
--- PASS: TestMultipleStartStop (XX.XXs)

=== RUN   TestGoroutineLeakDetection
    manager_test.go:XX: Initial goroutines: 5
    manager_test.go:XX: Goroutines after Start: 7 (added: 2)
    manager_test.go:XX: Goroutines after Stop: 5
    manager_test.go:XX: ✓ No goroutine leak detected
--- PASS: TestGoroutineLeakDetection (XX.XXs)

...

ok      smartdnssort/recursor   XX.XXs
```

---

## 故障排查

### 如果测试失败

1. **"send on closed channel" 错误**
   - 表示 stopCh 复用问题未完全修复
   - 检查 Stop() 中是否正确创建了新的 stopCh

2. **Goroutine 泄漏**
   - 表示 context 取消未正确实现
   - 检查 monitorCancel 和 healthCancel 是否被调用

3. **多个 healthCheckLoop 运行**
   - 表示循环依赖问题未修复
   - 检查重启后是否立即返回

4. **进程不重启**
   - 检查 healthCheckLoop 是否正确处理 exitCh
   - 检查 enabled 标志是否正确设置

---

## 总结

这份测试指南涵盖了所有三个高优先级修复的验证。建议按顺序执行这些测试，确保修复的正确性和完整性。
