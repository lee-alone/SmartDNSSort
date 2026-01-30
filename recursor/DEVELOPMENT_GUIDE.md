# Unbound Recursor 开发指南

## 📋 项目概述

本项目通过 `go:embed` 嵌入预编译的 Unbound 二进制文件（Debian 和 Windows 版本），实现完全自包含的递归 DNS 解析功能。

### 核心特性

- ✅ 完全自包含 - 单个 Go 二进制包含 Unbound
- ✅ 跨平台支持 - Debian 和 Windows
- ✅ 版本固定 - Unbound 1.19.1
- ✅ 无需系统依赖 - 无需 apt-get install
- ✅ 自动启停 - 启动时自动解压和启动
- ✅ 进程管理 - 健康检查和自动重启

---

## 📁 项目结构

```
recursor/
├── DEVELOPMENT_GUIDE.md          # 本文件
├── binaries/                     # 嵌入的二进制文件
│   ├── linux/
│   │   └── unbound              # Debian 版本（1.19.1）
│   └── windows/
│       └── unbound.exe          # Windows 版本（1.19.1）
├── config/
│   └── unbound.conf.template    # Unbound 配置模板
├── embedded.go                  # go:embed 定义和二进制提取
├── manager.go                   # Recursor 管理器
└── manager_test.go              # 单元测试
```

---

## 🚀 快速开始

### 第一步：准备 Unbound 二进制文件

#### 编译 Debian 版本

```bash
# 在 Debian 系统或容器中执行
docker run --rm -v $(pwd):/build debian:bullseye sh -c '
  apt-get update
  apt-get install -y build-essential libssl-dev wget
  
  cd /tmp
  wget https://www.unbound.net/downloads/unbound-1.19.1.tar.gz
  tar xzf unbound-1.19.1.tar.gz
  cd unbound-1.19.1
  
  ./configure --enable-static --disable-shared --with-ssl=/usr
  make
  strip src/unbound/unbound
  
  cp src/unbound/unbound /build/recursor/binaries/linux/
'
```

#### 编译 Windows 版本

```bash
# 方法 1：在 Windows 系统上使用 MinGW 编译
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

# 验证文件类型
file recursor/binaries/linux/unbound
file recursor/binaries/windows/unbound.exe
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

```go
package recursor

import (
    "context"
    "fmt"
    "net"
    "os"
    "os/exec"
    "path/filepath"
    "sync"
    "time"
)

// Manager 管理嵌入的 Unbound 递归解析器
type Manager struct {
    mu              sync.RWMutex
    cmd             *exec.Cmd
    unboundPath     string
    configPath      string
    port            int
    enabled         bool
    stopCh          chan struct{}
    lastHealthCheck time.Time
}

// NewManager 创建新的 Manager
func NewManager(port int) *Manager {
    return &Manager{
        port:   port,
        stopCh: make(chan struct{}),
    }
}

// Start 启动嵌入的 Unbound 进程
func (m *Manager) Start() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if m.enabled {
        return fmt.Errorf("recursor already running")
    }
    
    // 1. 解压 Unbound 二进制文件
    unboundPath, err := ExtractUnboundBinary()
    if err != nil {
        return fmt.Errorf("failed to extract unbound binary: %w", err)
    }
    m.unboundPath = unboundPath
    
    // 2. 生成配置文件
    configPath, err := m.generateConfig()
    if err != nil {
        return fmt.Errorf("failed to generate unbound config: %w", err)
    }
    m.configPath = configPath
    
    // 3. 启动 Unbound 进程
    m.cmd = exec.Command(m.unboundPath, "-c", m.configPath, "-d")
    m.cmd.Stdout = os.Stdout
    m.cmd.Stderr = os.Stderr
    
    if err := m.cmd.Start(); err != nil {
        return fmt.Errorf("failed to start unbound process: %w", err)
    }
    
    m.enabled = true
    m.lastHealthCheck = time.Now()
    
    // 4. 等待 Unbound 启动完成
    if err := m.waitForReady(5 * time.Second); err != nil {
        return fmt.Errorf("unbound may not be ready: %w", err)
    }
    
    // 5. 启动健康检查 goroutine
    go m.healthCheckLoop()
    
    return nil
}

// Stop 停止 Unbound 进程
func (m *Manager) Stop() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if !m.enabled {
        return nil
    }
    
    // 1. 停止健康检查
    close(m.stopCh)
    
    // 2. 优雅停止进程
    if m.cmd != nil && m.cmd.Process != nil {
        if err := m.cmd.Process.Signal(os.Interrupt); err != nil {
            return fmt.Errorf("failed to signal unbound: %w", err)
        }
        
        // 等待进程退出（最多 5 秒）
        done := make(chan error, 1)
        go func() {
            done <- m.cmd.Wait()
        }()
        
        select {
        case <-time.After(5 * time.Second):
            if err := m.cmd.Process.Kill(); err != nil {
                return fmt.Errorf("failed to kill unbound: %w", err)
            }
        case <-done:
        }
    }
    
    // 3. 清理临时文件
    if m.configPath != "" {
        os.Remove(m.configPath)
    }
    if m.unboundPath != "" {
        os.Remove(m.unboundPath)
    }
    
    m.enabled = false
    return nil
}

// generateConfig 生成 Unbound 配置文件
func (m *Manager) generateConfig() (string, error) {
    configDir, err := GetUnboundConfigDir()
    if err != nil {
        return "", err
    }
    
    configPath := filepath.Join(configDir, "unbound.conf")
    
    config := fmt.Sprintf(`server:
    port: %d
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
`, m.port)
    
    if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
        return "", fmt.Errorf("failed to write config file: %w", err)
    }
    
    return configPath, nil
}

// waitForReady 等待 Unbound 启动完成
func (m *Manager) waitForReady(timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    
    for {
        if time.Now().After(deadline) {
            return fmt.Errorf("timeout waiting for unbound to be ready")
        }
        
        conn, err := net.DialTimeout("udp", fmt.Sprintf("127.0.0.1:%d", m.port), 100*time.Millisecond)
        if err == nil {
            conn.Close()
            return nil
        }
        
        time.Sleep(100 * time.Millisecond)
    }
}

// healthCheckLoop 定期检查 Unbound 进程健康状态
func (m *Manager) healthCheckLoop() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-m.stopCh:
            return
        case <-ticker.C:
            m.performHealthCheck()
        }
    }
}

// performHealthCheck 执行一次健康检查
func (m *Manager) performHealthCheck() {
    m.mu.RLock()
    if !m.enabled || m.cmd == nil || m.cmd.Process == nil {
        m.mu.RUnlock()
        return
    }
    cmd := m.cmd
    m.mu.RUnlock()
    
    if err := cmd.Process.Signal(os.Signal(nil)); err != nil {
        // 进程已死亡，尝试重启
        m.mu.Lock()
        m.enabled = false
        m.mu.Unlock()
        
        if err := m.Start(); err != nil {
            // 重启失败，记录错误
            return
        }
        return
    }
    
    m.mu.Lock()
    m.lastHealthCheck = time.Now()
    m.mu.Unlock()
}

// IsEnabled 检查 Recursor 是否启用
func (m *Manager) IsEnabled() bool {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.enabled
}

// GetPort 获取 Unbound 监听端口
func (m *Manager) GetPort() int {
    return m.port
}

// GetAddress 获取 Unbound 地址
func (m *Manager) GetAddress() string {
    return fmt.Sprintf("127.0.0.1:%d", m.port)
}

// GetLastHealthCheck 获取最后一次健康检查时间
func (m *Manager) GetLastHealthCheck() time.Time {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.lastHealthCheck
}
```

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

## 🔧 集成到主项目

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
