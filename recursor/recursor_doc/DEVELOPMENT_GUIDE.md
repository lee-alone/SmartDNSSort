# Unbound Recursor 开发指南

## 📋 项目概述

本项目通过 `go:embed` 嵌入预编译的 Unbound 二进制文件（Debian x64 和 Windows x64 版本），实现完全自包含的递归 DNS 解析功能。

### 核心特性

- ✅ 完全自包含 - 单个 Go 二进制包含 Unbound
- ✅ 跨平台支持 - Debian x64 和 Windows x64
- ✅ 版本固定 - Unbound 1.24.2
- ✅ 无需系统依赖 - 无需 apt-get install
- ✅ 自动启停 - 启动时自动解压和启动
- ✅ 进程管理 - 健康检查和自动重启
- ✅ 动态配置 - 根据 CPU 核数自动调整参数

---

## 📁 项目结构

```
recursor/
├── DEVELOPMENT_GUIDE.md          # 本文件
├── binaries/                     # 嵌入的二进制文件（仅 x64）
│   ├── linux/
│   │   └── unbound              # Debian x64 版本（1.24.2）
│   └── windows/
│       └── unbound.exe          # Windows x64 版本（1.24.2）
├── data/
│   └── root.key                 # DNSSEC 信任锚
├── embedded.go                  # go:embed 定义和二进制提取
├── manager.go                   # Recursor 管理器
└── manager_test.go              # 单元测试
```

---

## 🚀 快速开始

### 第一步：准备 Unbound 二进制文件

仅支持 **Linux x64** 和 **Windows x64** 架构。

#### 编译 Debian x64 版本

```bash
# 在 Debian x64 系统或容器中执行
docker run --rm -v $(pwd):/build debian:bookworm sh -c '
  apt-get update
  apt-get install -y build-essential libssl-dev wget
  
  cd /tmp
  wget https://www.unbound.net/downloads/unbound-1.24.2.tar.gz
  tar xzf unbound-1.24.2.tar.gz
  cd unbound-1.24.2
  
  ./configure --enable-static --disable-shared --with-ssl=/usr
  make
  strip src/unbound/unbound
  
  cp src/unbound/unbound /build/recursor/binaries/linux/
'
```

#### 编译 Windows x64 版本

```bash
# 方法 1：在 Windows x64 系统上使用 MinGW 编译
# 方法 2：下载预编译版本
# https://www.unbound.net/download.html

# 将编译后的 unbound.exe 放入
# recursor/binaries/windows/unbound.exe
```

### 第二步：验证二进制文件

```bash
# 验证文件存在
ls -lh recursor/binaries/linux/unbound
ls -lh recursor/binaries/windows/unbound.exe

# 验证文件类型（应该都是 x64）
file recursor/binaries/linux/unbound
file recursor/binaries/windows/unbound.exe

# 输出示例：
# recursor/binaries/linux/unbound: ELF 64-bit LSB executable, x64, ...
# recursor/binaries/windows/unbound.exe: PE32+ executable (console) x64, ...
```

### 第三步：编译 Go 项目

```bash
# 编译
go build -o smartdnssort cmd/main.go

# 验证二进制大小
ls -lh smartdnssort
```

### 第四步：测试运行

```bash
# 启动服务
./smartdnssort -c config.yaml

# 在另一个终端测试
dig @127.0.0.1 -p 53 google.com
```

---

## 📝 文件说明

### recursor/embedded.go

定义 go:embed 和二进制提取逻辑。

```go
package recursor

import (
    "embed"
    "fmt"
    "os"
    "path/filepath"
    "runtime"
)

//go:embed binaries/*
var unboundBinaries embed.FS

// ExtractUnboundBinary 将嵌入的 unbound 二进制文件解压到临时目录
func ExtractUnboundBinary() (string, error) {
    platform := runtime.GOOS
    arch := runtime.GOARCH
    
    // 确定二进制文件名
    binName := "unbound"
    if platform == "windows" {
        binName = "unbound.exe"
    }
    
    // 构建嵌入文件路径
    binPath := filepath.Join("binaries", platform, binName)
    
    // 读取嵌入的二进制文件
    data, err := unboundBinaries.ReadFile(binPath)
    if err != nil {
        return "", fmt.Errorf("unbound binary not found for %s: %w", platform, err)
    }
    
    // 创建临时目录
    tmpDir := filepath.Join(os.TempDir(), "smartdnssort-unbound")
    if err := os.MkdirAll(tmpDir, 0755); err != nil {
        return "", fmt.Errorf("failed to create temp directory: %w", err)
    }
    
    // 写入二进制文件
    outPath := filepath.Join(tmpDir, binName)
    if err := os.WriteFile(outPath, data, 0755); err != nil {
        return "", fmt.Errorf("failed to write unbound binary: %w", err)
    }
    
    return outPath, nil
}

// GetUnboundConfigDir 获取 Unbound 配置目录
func GetUnboundConfigDir() (string, error) {
    tmpDir := filepath.Join(os.TempDir(), "smartdnssort-unbound")
    if err := os.MkdirAll(tmpDir, 0755); err != nil {
        return "", fmt.Errorf("failed to create config directory: %w", err)
    }
    return tmpDir, nil
}

// CleanupUnboundFiles 清理临时文件
func CleanupUnboundFiles() error {
    tmpDir := filepath.Join(os.TempDir(), "smartdnssort-unbound")
    if err := os.RemoveAll(tmpDir); err != nil {
        return fmt.Errorf("failed to cleanup unbound files: %w", err)
    }
    return nil
}
```

### recursor/manager.go

管理 Unbound 进程的生命周期。

**关键特性：**

- 动态配置生成：根据 CPU 核数自动调整线程数和缓存大小
- 自动提取 root.key：从嵌入的数据中提取 DNSSEC 信任锚
- 跨平台健康检查：使用 `cmd.Wait()` 而非 Signal 检查，确保 Windows 兼容性
- 自动重启：进程异常退出时自动重启
- 优雅关闭：发送 SIGTERM 信号，等待进程退出

**进程生命周期管理：**

```
启动流程：
1. 解压二进制文件
2. 提取 root.key
3. 生成动态配置
4. 启动 Unbound 进程
5. 启动 goroutine 等待进程退出（cmd.Wait()）
6. 等待端口就绪
7. 启动健康检查循环

健康检查：
- 使用 channel 接收进程退出事件（跨平台兼容）
- 不使用 Signal(nil) 检查（Windows 不可靠）
- 进程退出时自动重启
- 定期更新最后检查时间
```

**配置动态调整逻辑：**

```go
// Go 1.21+ 现代语法
numThreads := max(1, min(runtime.NumCPU(), 8))

// 缓存大小计算
msgCacheSize := 50 + (25 * numThreads)     // 基础 50m + 每线程 25m
rrsetCacheSize := 100 + (50 * numThreads)  // 基础 100m + 每线程 50m
```

**示例：**
- 4核机器：4线程，150m消息缓存，300m RRSET缓存
- 8核机器：8线程，250m消息缓存，500m RRSET缓存
- 16核机器：8线程（上限），250m消息缓存，500m RRSET缓存

### recursor/config/unbound.conf.template

Unbound 配置模板（可选，如果需要更复杂的配置）。

```
server:
    port: 5353
    do-ip4: yes
    do-ip6: no
    do-udp: yes
    do-tcp: yes
    
    interface: 127.0.0.1
    
    num-threads: 4
    msg-cache-size: 100m
    rrset-cache-size: 200m
    cache-min-ttl: 60
    cache-max-ttl: 86400
    
    module-config: "validator iterator"
    
    verbosity: 1
    log-queries: no
    log-replies: no
    
    hide-identity: yes
    hide-version: yes
    
    access-control: 127.0.0.1 allow
    access-control: ::1 allow
    access-control: 0.0.0.0/0 deny
    access-control: ::/0 deny
```

---

## 🔧 技术细节

### Windows 兼容性修复

**问题：** 原始代码使用 `os.Process.Signal(nil)` 检查进程存活性，但在 Windows 上不可靠，导致误判进程已死亡，造成无限重启循环。

**解决方案：** 使用 `cmd.Wait()` 的 goroutine + channel 方案：

```go
// 启动 goroutine 等待进程退出
go func() {
    exitErr := m.cmd.Wait()
    m.exitCh <- exitErr
}()

// 在健康检查循环中接收退出事件
select {
case exitErr := <-m.exitCh:
    // 进程已退出，尝试重启
    if err := m.Start(); err != nil {
        logger.Errorf("Failed to restart unbound: %v", err)
    }
}
```

**优点：**
- ✅ 跨平台兼容（Unix 和 Windows）
- ✅ 准确捕获进程退出事件
- ✅ 避免僵尸进程
- ✅ 事件驱动而非轮询，更高效

### 健康检查策略

**当前设计：**
- `healthCheckLoop` 监听 `exitCh` 捕获进程崩溃
- `performHealthCheck()` 仅更新最后检查时间戳
- 不执行主动的端口连通性检查

**为什么这样设计：**
1. **进程崩溃检测**：`cmd.Wait()` 能准确捕获进程异常退出
2. **资源效率**：避免频繁的网络 I/O 操作
3. **简化逻辑**：exitCh 已覆盖主要故障场景

**未来扩展方向：**
如果需要检测"进程僵死"（进程存在但不响应），可在 `performHealthCheck()` 中添加 UDP Dial 检查：

```go
func (m *Manager) performHealthCheck() {
    m.mu.Lock()
    m.lastHealthCheck = time.Now()
    m.mu.Unlock()
    
    // 可选：检测进程是否响应
    conn, err := net.DialTimeout("udp", m.GetAddress(), 500*time.Millisecond)
    if err != nil {
        logger.Warnf("[Recursor] Port check failed: %v", err)
        // 可以在这里触发重启逻辑
        return
    }
    conn.Close()
}
```

---

### 1. 在 dnsserver/server.go 中添加

```go
type Server struct {
    // ... 现有字段
    recursorMgr *recursor.Manager
}
```

### 2. 在 dnsserver/server_init.go 中初始化

```go
func NewServer(cfg *config.Config, s *stats.Stats) *Server {
    // ... 现有代码
    
    server := &Server{
        // ... 现有初始化
    }
    
    // 初始化递归解析器
    if cfg.Upstream.EnableRecursor {
        recursorPort := cfg.Upstream.RecursorPort
        if recursorPort == 0 {
            recursorPort = 5353
        }
        server.recursorMgr = recursor.NewManager(recursorPort)
    }
    
    return server
}
```

### 3. 在 dnsserver/server_lifecycle.go 中启动/停止

```go
func (s *Server) Start() error {
    // ... 现有代码
    
    // 启动递归解析器
    if s.recursorMgr != nil {
        if err := s.recursorMgr.Start(); err != nil {
            logger.Warnf("Failed to start recursor: %v", err)
        }
    }
    
    // ... 其他启动代码
}

func (s *Server) Shutdown() {
    // ... 现有代码
    
    // 停止递归解析器
    if s.recursorMgr != nil {
        if err := s.recursorMgr.Stop(); err != nil {
            logger.Warnf("Failed to stop recursor: %v", err)
        }
    }
    
    // ... 其他关闭代码
}
```

### 4. 在 upstream/manager.go 中添加 Recursor 作为上游源

```go
// 在 NewManager 中，初始化后添加：
if cfg.EnableRecursor {
    recursorAddr := fmt.Sprintf("127.0.0.1:%d", cfg.RecursorPort)
    recursorUpstream := NewSimpleUpstream(recursorAddr)
    manager.servers = append(manager.servers, recursorUpstream)
}
```

### 5. 在 config/config_types.go 中添加配置

```go
type UpstreamConfig struct {
    Servers []string `yaml:"servers,omitempty" json:"servers"`
    
    // 新增：启用嵌入式递归解析器
    EnableRecursor bool `yaml:"enable_recursor,omitempty" json:"enable_recursor"`
    
    // 新增：递归解析器端口
    RecursorPort int `yaml:"recursor_port,omitempty" json:"recursor_port"`
    
    // ... 其他字段
}
```

---

## 📋 配置示例

### config.yaml

```yaml
upstream:
  servers:
    - "8.8.8.8:53"
    - "1.1.1.1:53"
  
  # 启用嵌入式递归解析器
  enable_recursor: true
  recursor_port: 5353
  
  strategy: "parallel"
  timeout_ms: 5000
```

---

## 🧪 测试

### 单元测试

```bash
# 运行测试
go test ./recursor -v

# 运行特定测试
go test ./recursor -v -run TestManager
```

### 集成测试

```bash
# 启动服务
./smartdnssort -c config.yaml

# 测试 DNS 查询
dig @127.0.0.1 -p 53 google.com
dig @127.0.0.1 -p 53 example.com

# 测试本地 Unbound
dig @127.0.0.1 -p 5353 google.com
```

---

## 📊 性能指标

### 文件大小

```
Unbound 二进制（Debian）：6-10MB
Unbound 二进制（Windows）：6-10MB
总增加大小：12-20MB
```

### 启动时间

```
解压二进制：< 1 秒
生成配置：< 0.1 秒
启动 Unbound：1-2 秒
总计：2-3 秒
```

### 内存占用

```
启动时：50-100MB
运行 1 小时：50-150MB
```

---

## 🔍 故障排查

### 问题 1：Unbound 启动失败

```
错误：failed to extract unbound binary

解决：
1. 检查 recursor/binaries/ 目录
2. 确保二进制文件存在
3. 检查文件权限
```

### 问题 2：端口被占用

```
错误：address already in use

解决：
1. 修改 recursor_port 配置
2. 或杀死占用端口的进程
```

### 问题 3：DNS 查询失败

```
错误：upstream query failed

解决：
1. 检查 Unbound 进程是否运行
2. 测试本地连接：dig @127.0.0.1 -p 5353
3. 查看日志输出
```

---

## 📚 相关文件

- `recursor/embedded.go` - go:embed 定义和二进制提取
- `recursor/manager.go` - Recursor 管理器
- `recursor/manager_test.go` - 单元测试
- `recursor/config/unbound.conf.template` - 配置模板

---

## 🎯 下一步

1. ✅ 准备 Unbound 二进制文件
2. ✅ 创建 recursor 包
3. ✅ 集成到主项目
4. ✅ 配置和测试
5. ✅ 部署到生产环境

---

**开发指南完成！** 👍
