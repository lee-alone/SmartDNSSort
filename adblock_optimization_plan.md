# AdBlock SuffixMatcher 优化改造计划

## 概述

本改造计划旨在提升 AdBlock 模块中 `SuffixMatcher` 的性能，通过将底层实现从低效的 `map` 循环查找替换为高效的 `hashicorp/go-immutable-radix` 基数树（Radix Tree）。此举将使后缀匹配操作的复杂度从近似 O(L²) 降低到严格 O(L)，显著减少 CPU 开销和内存分配，从而全面提升 AdBlock 功能在高并发场景下的性能和稳定性。

## 前提条件

### 1. 添加依赖

在项目根目录下执行以下命令，添加 `go-immutable-radix` 库：

```bash
go get github.com/hashicorp/go-immutable-radix
```

## 改造步骤

### 文件: `adblock/matcher.go`

这是核心修改文件。

#### 1.1. 引入依赖并修改 `SuffixMatcher` 结构体

*   引入 `github.com/hashicorp/go-immutable-radix` 包。
*   将 `SuffixMatcher` 结构体中的 `rules map[string]struct{}` 替换为 `*radix.Tree`。
*   将 `sync.RWMutex` 替换为 `sync.Mutex`，因为 `go-immutable-radix` 的读取操作本身是并发安全的，`Mutex` 仅用于保护树的写入（指针更新）。

```go
// adblock/matcher.go

package adblock

import (
	"strings"
	"sync"

	radix "github.com/hashicorp/go-immutable-radix" // 引入新库
)

// ... (ExactMatcher 和 HostsMatcher 的代码保持不变) ...

// SuffixMatcher 后缀匹配器 (使用 Radix Tree 重构)
type SuffixMatcher struct {
    tree *radix.Tree
    mu   sync.Mutex // 仅用于保护写操作（更新 tree 指针）
}
```

#### 1.2. 修改 `NewSuffixMatcher` 构造函数

*   初始化 `SuffixMatcher` 时，创建一个空的 Radix 树。

```go
// adblock/matcher.go

// NewSuffixMatcher 创建一个新的后缀匹配器
func NewSuffixMatcher() *SuffixMatcher {
    return &SuffixMatcher{
        tree: radix.New(), // 初始化一个空的 Radix Tree
    }
}
```

#### 1.3. 重写 `AddRule` 方法

*   将域名颠倒（例如 `example.com` -> `com.example`），以便 Radix Tree 进行前缀匹配（对应后缀规则）。
*   调用 `m.tree.Insert()` 方法，该方法返回一个新的树。通过 `m.tree = newTree` 原子地更新树的指针。

```go
// adblock/matcher.go

// AddRule 添加一条后缀匹配规则
// 输入应该是纯域名部分，例如 "example.com" (来自 ||example.com^)
func (m *SuffixMatcher) AddRule(domain string) {
    // 1. 颠倒域名，以便进行前缀匹配 (e.g., "example.com" -> "com.example")
    parts := strings.Split(domain, ".")
    for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
        parts[i], parts[j] = parts[j], parts[i]
    }
    reversedDomain := strings.Join(parts, ".")

    // 2. 锁定并更新 Radix 树
    m.mu.Lock()
    defer m.mu.Unlock()

    // Insert 操作返回一个新的树，这是实现不可变性的关键。
    // 为了简化存储，只存储 true 作为标记，避免不必要的字符串存储。
    newTree, _, _ := m.tree.Insert([]byte(reversedDomain), true)
    m.tree = newTree // 原子地替换树的指针
}
```

#### 1.4. 重写 `Match` 方法

*   将待查询的域名颠倒。
*   使用 Radix Tree 的 `Root().LongestPrefix()` 方法进行查找，它能高效地找到最长的匹配前缀（对应于原始域名的最短匹配后缀）。

```go
// adblock/matcher.go

// Match 检查域名是否匹配后缀规则
func (m *SuffixMatcher) Match(domain string) (bool, string) {
    // 1. 颠倒待查询的域名 (e.g., "sub.example.com" -> "com.example.sub")
    parts := strings.Split(domain, ".")
    for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
        parts[i], parts[j] = parts[j], parts[i]
    }
    reversedDomain := strings.Join(parts, ".")

    // 2. 使用 Radix Tree 的 LongestPrefix 方法进行高效查找。
    // LongestPrefix 会找到树中与 reversedDomain 拥有最长共同前缀的那个 key。
    // 这正是我们需要的后缀匹配逻辑。
    // 由于读取是并发安全的，这里不需要加锁。
    _, value, found := m.tree.Root().LongestPrefix([]byte(reversedDomain))

    if found {
        // 找到了匹配
        return true, "||" + domain + "^"
    }

    return false, ""
}
```

#### 1.5. 修改 `Count` 方法

*   `Count` 方法现在应该返回 Radix 树中存储的条目数量。

```go
// adblock/matcher.go

// Count 返回规则数量
func (m *SuffixMatcher) Count() int {
    // Radix Tree 的 Len() 方法是线程安全的
    return m.tree.Len()
}
```

#### 1.6. 并发写入保护注意事项

**重要**: 当前 `AddRule` 中的 `mu.Lock()` 保护树指针更新是正确的，但需要注意：

*   如果多个 goroutine 同时调用 `AddRule`，可能存在**写入竞态**问题：第一个获得锁的 goroutine 基于某个版本的树创建新树，而其他 goroutine 可能基于旧版本，导致某些更新被覆盖。
*   **建议方案**：
    - 在规则批量加载阶段使用单线程，即将所有 `AddRule` 调用集中在一个 goroutine 中
    - 或使用 CAS（Compare-And-Swap）机制确保只有最新的树被保留
    - 例如：
    ```go
    newTree, _, _ := m.tree.Insert([]byte(reversedDomain), true)
    // 使用 atomic.CompareAndSwapPointer 确保只有一个 goroutine 成功更新
    for !atomic.CompareAndSwapPointer((*unsafe.Pointer)(unsafe.Pointer(&m.tree)), 
        unsafe.Pointer(m.tree), unsafe.Pointer(newTree)) {
        // 重试逻辑
    }
    ```
    - 更简单的做法：在规则更新时，暂时禁止并发读写（通过 RWMutex），这样在批量更新时会更安全

### 文件: `adblock` 规则解析部分

#### 2.1. 验证规则解析逻辑

*   **检查**: 请确认 `adblock` 包中负责**解析 AdBlock 规则文件** (例如 `filter.go` 或 `rule_loader.go`)，并最终调用 `SuffixMatcher.AddRule` 的那部分代码。
*   **目标**: 确保传递给 `AddRule` 的参数仍然是**纯域名**。例如，如果规则文件中有一条规则是 `||example.com^`，那么传递给 `AddRule` 的字符串应该是 `example.com`（不包含 `||` 和 `^`）。
*   从您现有的代码结构和 `AddRule` 的设计来看，这部分逻辑很可能不需要修改，但验证一下会更保险。

## 测试与验证

### 单元测试

1.  **基础功能测试**: 运行现有的 `*_test.go` 文件，确保新实现通过所有测试。
2.  **边界情况测试**: 添加以下边界用例
    ```go
    // 单级域名
    matcher.AddRule("com")
    assert.True(t, matcher.Match("com"))
    
    // 多级域名
    matcher.AddRule("example.com")
    assert.True(t, matcher.Match("example.com"))
    assert.True(t, matcher.Match("sub.example.com"))
    assert.False(t, matcher.Match("example.org"))
    
    // 含数字和特殊字符
    matcher.AddRule("test-123.example.co.uk")
    assert.True(t, matcher.Match("test-123.example.co.uk"))
    
    // 完整的二级域名后缀
    matcher.AddRule("co.uk")
    assert.True(t, matcher.Match("example.co.uk"))
    assert.False(t, matcher.Match("example.com"))
    ```
3.  **并发测试**: 测试多个 goroutine 同时读写时的数据一致性
    ```go
    // 添加规则和查询并发运行，验证无竞态
    ```

### 功能测试

*   部署改造后的版本，进行 AdBlock 功能的全面测试。
*   验证 AdBlock 规则的更新和重载功能是否正常。
*   确保各种域名格式能被正确拦截或放行。

### 性能测试

*   在模拟高 QPS 负载的环境下，观察 AdBlock 相关的 CPU 使用率和 DNS 查询延迟。
*   与改造前的版本进行对比，验证性能提升是否达到预期。
*   重点关注：
    - 查询延迟的 p50、p95、p99 分位数
    - 垃圾回收（GC）的频率和暂停时间
    - 内存占用变化

## 回滚方案

为了降低改造风险，建议制定以下回滚策略：

1.  **Feature Flag 方案**（推荐）
    *   在配置中添加开关 `use_radix_tree_suffix_matcher`，默认为 `false`
    *   同时保留原有的 `map` 实现（重命名为 `SuffixMatcherLegacy`）
    *   根据配置项动态选择使用哪个实现
    *   这样即使发现问题，也可以快速切换回旧实现而无需重新编译

2.  **分支版本管理**
    *   在 Git 中创建 feature 分支进行开发和测试
    *   只有性能测试和功能测试都通过后，才合并到主分支
    *   保留旧实现的代码分支以备需要

3.  **灰度发布**（如果有多个实例）
    *   先在 10% 的实例上部署新版本，观察 48 小时
    *   如无异常，逐步扩大到 50%、100%
    *   保留快速回滚的能力

## 预期收益

*   **性能显著提升**: AdBlock 后缀匹配的算法复杂度从近似 O(L²) 降低到严格 O(L)，尤其在处理复杂域名时，性能提升将非常明显。
*   **降低 CPU 占用**: 匹配过程中的字符串创建和拼接操作大幅减少，从而减轻 Go 垃圾回收器的压力，降低整体 CPU 占用。
*   **提高系统稳定性**: GC 压力的减轻意味着更少的 GC 暂停，使得系统在高负载下表现更稳定，平均延迟更低。
*   **高并发安全**: `go-immutable-radix` 的不可变特性确保了在规则更新时，读操作无需加锁，进一步提升了并发性能。

## 建议执行顺序

1.  **第一步**：添加依赖和修改 `matcher.go` - 按上述改造步骤执行
2.  **第二步**：编写广泛的单元测试（特别是反转逻辑的边界情况）
3.  **第三步**：验证 `rule_loader.go` 中调用 `AddRule` 的部分，确保传入的都是纯域名
4.  **第四步**：在测试环境中验证功能，运行现有的 `*_test.go` 文件
5.  **第五步**：性能对比测试 - 用相同的规则集和查询模式进行压测
6.  **第六步**：灰度发布（如果有）或完整切换

---

