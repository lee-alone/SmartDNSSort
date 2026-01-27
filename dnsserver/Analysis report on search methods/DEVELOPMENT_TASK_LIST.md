# SmartDNSSort 性能优化与自适应改造 - 开发任务清单

## 📋 概览

根据前期论证结论，本次改造旨在通过“自适应自动化”取代“繁琐的手动调优”，同时修复已识别的性能瓶颈。开发人员需按以下阶段完成工作，确保系统在复杂环境下保持高性能与稳健性。

---

## 第一阶段：配置重心转移与基础设施改造
**目标**：实现“参数消除”与“用户覆盖机制”，打好自适应基础。

1.  **重构配置结构体 (`config/config_types.go`)**：
    *   将可选参数（如 `Concurrency`, `MaxConnections`, `SequentialTimeoutMs` 等）改为**指针类型**（`*int`），以便区分“未配置”与“显式配置”。
    *   新增 `DynamicParamOptimization` 结构体，包含 `EWMAAlpha`、`MaxStepMs` 等平滑处理参数。
2.  **实现自适应初始化逻辑 (`upstream/manager.go`)**：
    *   在 `NewManager` 中实现三层优先级逻辑：用户配置 > 自动计算 (`NumCPU` 相关公式) > 硬编码默认值。
    *   实现策略自适应：若 `strategy` 为空或 `"auto"`，根据服务器数量自动分配模式（1: Sequential, 2-3: Racing, 4+: Parallel）。
3.  **连接池自适应改造 (`upstream/transport/connection_pool.go`)**：
    *   修改 `NewConnectionPool`，使其 `MaxConnections` 默认基于 `runtime.NumCPU() * 5` 自动计算（最低 20）。

---

## 第二阶段：核心查询逻辑与性能补丁
**目标**：消除代码层面的硬伤，提升响应速度和成功率。

4.  **修复并行查询信号量瓶颈 (`upstream/manager_parallel.go`)**：
    *   在 `queryParallel` 函数中，确保信号量 channel 的缓冲区大小 `concurrency` 至少等于当前服务器总数，防止不必要的内部排队。
5.  **实现后台收集超时控制 (`upstream/manager_parallel.go`)**：
    *   在 `collectRemainingResponses` 中引入 `context.WithTimeout`（建议 2s），防止后台协程因长耗时上游导致无限挂起。
6.  **优化顺序/竞速查询超时控制**：
    *   `Sequential` (`manager_sequential.go`)：单次尝试超时从 1.5s 减至 1s，或基于 `timeoutMs / len(servers)` 动态分配。
    *   `Racing` (`manager_racing.go`)：将硬编码的 `100ms` 延迟调整为调用内部 `getRacingDelay()` 动态获取平滑后的延迟值。

---

## 第三阶段：动态参数平滑与稳健性增强
**目标**：通过数学平滑手段防止系统在抖动网络环境下出现震荡。

7.  **实现 EWMA 平滑算法 (`upstream/manager.go`)**：
    *   为 `Manager` 添加 `smoothedRacingDelay` 状态字段及 `racingDelayAlpha` 平滑系数。
    *   实现核心公式：`smoothed = α * newValue + (1 - α) * oldValue`。
8.  **引入更新步长与频率限制**：
    *   增加 `clamp` 逻辑，限制单次参数变化幅度（例如最大步长 5ms）。
    *   实现最小更新时间间隔（1s），确保参数计算不会过于频繁。
9.  **熔断机制指数退避 (`upstream/health.go`)**：
    *   改造 `ShouldSkipTemporarily` 逻辑，实现指数级恢复尝试时间（10s, 20s, 30s...），避免故障上游恢复初期的冲击。

---

## 第四阶段：可观测性、文档与自动化生成
**目标**：提升系统透明度，确保配置体验符合 Zero-Config 愿景。

10. **完善初始化日志记录**：
    *   在启动阶段打印每个关键参数的来源。示例：`"Using Auto-calculated MaxConnections: 40 (based on 8 CPUs)"`。
11. **实现配置模板生成器 (`config/config_generator.go`)**：
    *   创建包含详尽注释的 YAML 模板，将计算公式、推荐范围直接写在注释中。
    *   逻辑触发：若探测到配置文件缺失，自动生成一份带完整说明的默认 `config.yaml`。
12. **开发验证单元测试**：
    *   编写针对 EWMA 计算逻辑的数学单元测试。
    *   编写并发压力测试，在模拟高 QPS 环境下验证连接池扩容后的稳定性。

---

## 🎨 交付标准 (Definition of Done)

*   **配置简化**：标准部署下，用户仅需提供 `servers` 和 `timeoutMs` 即可获得最佳性能。
*   **平滑运行**：在模拟抖动延迟网络下，内部竞速参数变化轨迹平滑，无突变。
*   **无资源泄漏**：所有后台协程均受 Context 生命周期管理，且具备明确超时。
*   **性能提升**：吞吐量提升预估 50% 以上，连接池耗尽导致的失败率降低至 1% 以下。
