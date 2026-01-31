# Recursor 后端实现快速参考

## 🎯 实现概览

Recursor 后端集成已完成。DNS 服务器现在支持启用/禁用嵌入式 Unbound 递归解析器。

---

## 📝 修改的文件

### 1. `dnsserver/server.go`

**修改内容**：
- 添加导入：`"smartdnssort/recursor"`
- 添加字段：`recursorMgr *recursor.Manager`

**代码位置**：
```go
// 第 8 行：添加导入
import (
    // ...
    "smartdnssort/recursor"
)

// 第 35 行：添加字段
type Server struct {
    // ...
    recursorMgr *recursor.Manager
}
```

---

### 2. `dnsserver/server_init.go`

**修改内容**：
- 添加导入：`"smartdnssort/recursor"`
- 添加初始化逻辑

**代码位置**：
```go
// 第 8 行：添加导入
import (
    // ...
    "smartdnssort/recursor"
)

// 第 60 行：添加初始化代码
if cfg.Upstream.EnableRecursor {
    recursorPort := cfg.Upstream.RecursorPort
    if recursorPort == 0 {
        recursorPort = 5353
    }
    server.recursorMgr = recursor.NewManager(recursorPort)
    logger.Infof("[Recursor] Manager initialized for port %d", recursorPort)
}
```

---

### 3. `dnsserver/server_lifecycle.go`

**修改内容**：
- 在 `Start()` 中添加启动逻辑
- 在 `Shutdown()` 中添加关闭逻辑

**代码位置**：
```go
// Start() 函数中，第 30 行左右
if s.recursorMgr != nil {
    if err := s.recursorMgr.Start(); err != nil {
        logger.Warnf("[Recursor] Failed to start recursor: %v", err)
    } else {
        logger.Infof("[Recursor] Recursor started on %s", s.recursorMgr.GetAddress())
    }
}

// Shutdown() 函数中，第 40 行左右
if s.recursorMgr != nil {
    if err := s.recursorMgr.Stop(); err != nil {
        logger.Warnf("[Recursor] Failed to stop recursor: %v", err)
    } else {
        logger.Info("[Recursor] Recursor stopped successfully.")
    }
}
```

---

## 🔧 配置

### 启用 Recursor

**config.yaml**：
```yaml
upstream:
  enable_recursor: true
  recursor_port: 5353
```

### 禁用 Recursor

```yaml
upstream:
  enable_recursor: false
```

---

## 🌐 API 端点

### 获取状态

```bash
GET /api/recursor/status
```

**响应**：
```json
{
  "enabled": true,
  "port": 5353,
  "address": "127.0.0.1:5353",
  "uptime": 7200,
  "last_health_check": 1706700000
}
```

---

## 🚀 使用流程

### 1. 编译

```bash
go build -o smartdnssort cmd/main.go
```

### 2. 配置

编辑 `config.yaml`：
```yaml
upstream:
  servers:
    - "8.8.8.8:53"
  enable_recursor: true
  recursor_port: 5353
```

### 3. 启动

```bash
./smartdnssort -c config.yaml
```

### 4. 验证

```bash
# 检查 Recursor 状态
curl http://localhost:8080/api/recursor/status

# 测试 DNS 查询
dig @127.0.0.1 -p 53 google.com

# 测试本地 Recursor
dig @127.0.0.1 -p 5353 google.com
```

---

## 📊 生命周期

### 启动时

1. 读取配置
2. 创建 Recursor Manager（如果启用）
3. 启动 DNS 服务器
4. 启动 Unbound 进程
5. 启动健康检查

### 运行时

- DNS 查询处理
- Recursor 健康检查
- 进程崩溃自动重启

### 关闭时

1. 停止 Recursor
2. 关闭上游连接
3. 保存缓存
4. 清理临时文件

---

## 🔍 日志

### 启动成功

```
[INFO] [Recursor] Manager initialized for port 5353
[INFO] [Recursor] Recursor started on 127.0.0.1:5353
```

### 启动失败

```
[WARN] [Recursor] Failed to start recursor: address already in use
```

### 自动重启

```
[WARN] [Recursor] Process exited unexpectedly, attempting restart...
[INFO] [Recursor] Recursor started on 127.0.0.1:5353
```

### 关闭

```
[INFO] [Recursor] Recursor stopped successfully.
```

---

## ⚠️ 常见问题

### Q: 端口被占用怎么办？

**A**: 修改配置中的 `recursor_port`：
```yaml
upstream:
  recursor_port: 8053  # 改为其他端口
```

### Q: 启动失败怎么办？

**A**: 检查日志，常见原因：
- 端口被占用
- 权限不足（Linux 下使用 < 1024 的端口）
- 二进制文件缺失

### Q: 如何禁用 Recursor？

**A**: 在配置中设置：
```yaml
upstream:
  enable_recursor: false
```

### Q: 如何查看 Recursor 状态？

**A**: 调用 API：
```bash
curl http://localhost:8080/api/recursor/status
```

---

## 📋 验证清单

- [x] 代码编译成功
- [x] 配置系统支持
- [x] API 端点实现
- [x] 启动/关闭逻辑
- [x] 日志记录
- [ ] 前端集成
- [ ] 单元测试
- [ ] 集成测试

---

## 🔗 相关文件

| 文件 | 说明 |
|------|------|
| `recursor/manager.go` | Recursor 管理器 |
| `recursor/embedded.go` | 二进制提取 |
| `webapi/api_recursor.go` | API 端点 |
| `config/config_types.go` | 配置定义 |
| `dnsserver/server.go` | DNS 服务器 |
| `dnsserver/server_init.go` | 初始化逻辑 |
| `dnsserver/server_lifecycle.go` | 生命周期管理 |

---

## 📚 完整文档

详见：`RECURSOR_BACKEND_IMPLEMENTATION.md`

---

**最后更新**：2026-01-31  
**版本**：1.0  
**状态**：✅ 完成

