# CNAME 解析测试文档

## 问题描述
之前的程序在处理 CNAME 记录时，只返回了 CNAME 本身，而没有返回 CNAME 指向的 IP 地址。

## 修复内容

### 修改文件：`upstream/upstream.go`

#### 修改点：`QueryAll` 函数（第277-381行）

**主要改进：**

1. **检测 CNAME 记录**：首先扫描 Answer 段，查找 CNAME 记录
2. **收集目标域名的 IP**：如果有 CNAME，会在所有段（Answer、Extra、Ns）中查找 CNAME 目标域名的 IP 地址
3. **额外查询**：如果响应中没有包含 CNAME 目标的 IP，会主动发起额外的 A 和 AAAA 查询来获取 IP
4. **同时返回**：返回结果中包含 CNAME 和解析后的 IP 地址

**修改逻辑：**

```
原来的逻辑：
- 查询域名
- 如果没有 IP，检查是否有 CNAME
- 如果有 CNAME，只返回 CNAME（没有 IP）

现在的逻辑：
- 查询域名
- 首先检测是否有 CNAME
- 收集原始域名和 CNAME 目标域名的所有 IP
- 如果有 CNAME 但没有 IP，主动查询 CNAME 目标的 A 和 AAAA 记录
- 返回 CNAME 和完整的 IP 列表
```

## 预期效果

### 修复前
```
查询: example.com
返回: CNAME -> cdn.example.com (只有 CNAME，没有 IP)
客户端需要再次查询 cdn.example.com
```

### 修复后
```
查询: example.com
返回: 
  CNAME -> cdn.example.com
  IPs -> [1.2.3.4, 5.6.7.8, 2001:db8::1]
客户端可以直接使用 IP，不需要额外查询
```

## 测试建议

1. **测试带 CNAME 的域名**：
   - 查询一些使用 CDN 的域名（通常会有 CNAME）
   - 验证返回结果中既有 CNAME 也有 IP 地址

2. **测试不同场景**：
   - 响应中包含 CNAME + IP（Answer 段）
   - 响应中包含 CNAME，IP 在 Extra 段
   - 响应中只有 CNAME，需要额外查询

3. **查看日志输出**：
   - 启用详细日志，观察 CNAME 检测和 IP 收集过程
   - 检查是否有额外查询的日志

## 相关代码

- `upstream/upstream.go` - QueryAll 函数（主要修改）
- `dnsserver/server.go` - handleQuery 函数（处理查询结果）
