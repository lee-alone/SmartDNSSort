# Unbound Web 配置编辑 - 故障排查指南

## 问题：自定义设置中看不到 Unbound 配置编辑器

### 可能的原因和解决方案

#### 1. 递归功能未启用

**症状**：编辑器完全不显示

**原因**：Unbound 配置编辑器只在递归功能启用时显示

**解决**：
1. 打开「配置」标签页
2. 找到「上游」→「递归解析器」部分
3. 勾选「启用嵌入式 Unbound 递归解析器」
4. 保存配置
5. 返回「自定义设置」，应该能看到编辑器

#### 2. 页面未正确加载

**症状**：页面加载但编辑器不显示

**原因**：JavaScript 可能没有正确加载或执行

**解决**：
1. 打开浏览器开发者工具（F12）
2. 查看「Console」标签页
3. 查找错误信息
4. 刷新页面（Ctrl+F5 强制刷新）

#### 3. API 端点未正确注册

**症状**：编辑器显示但无法加载配置

**原因**：API 路由可能未正确注册

**解决**：
1. 打开浏览器开发者工具（F12）
2. 查看「Network」标签页
3. 刷新页面
4. 查找 `/api/unbound/config` 请求
5. 检查响应状态码：
   - 200 - 成功
   - 404 - 端点未找到
   - 400 - 递归未启用
   - 500 - 服务器错误

#### 4. 编译问题

**症状**：编译时出现错误

**原因**：代码可能有语法错误

**解决**：
1. 重新编译：`go build -o smartdnssort.exe ./cmd`
2. 检查编译错误信息
3. 确保所有文件都已正确修改

### 调试步骤

#### 步骤 1：检查递归是否启用

```bash
# 查看配置文件
cat config.yaml | grep -A 5 "upstream:"

# 查找 enable_recursor
grep "enable_recursor" config.yaml
```

#### 步骤 2：检查 API 响应

```bash
# 测试 API 端点
curl http://localhost:8080/api/unbound/config

# 应该返回类似的响应：
# {"content":"# Unbound configuration...","enabled":true}
# 或
# {"content":"","enabled":false}
```

#### 步骤 3：检查浏览器控制台

打开浏览器开发者工具（F12），查看 Console 标签页：

```javascript
// 手动测试加载函数
loadUnboundConfig();

// 查看是否有错误信息
```

#### 步骤 4：检查 HTML 元素

```javascript
// 检查编辑器元素是否存在
document.getElementById('unbound-config-section');
document.getElementById('unbound-config-content');

// 应该返回 HTML 元素对象，而不是 null
```

### 常见错误信息

#### 错误：`Recursor is not enabled`

**原因**：递归功能未启用

**解决**：启用递归功能

#### 错误：`Failed to read config file`

**原因**：配置文件不存在或无法读取

**解决**：
1. 检查 `unbound/unbound.conf` 文件是否存在
2. 检查文件权限
3. 重新启用递归功能

#### 错误：`Failed to write config file`

**原因**：无法写入配置文件

**解决**：
1. 检查目录权限
2. 确保 `unbound/` 目录可写
3. 检查磁盘空间

### 验证修复

修复后，按照以下步骤验证：

1. **重新编译**
   ```bash
   go build -o smartdnssort.exe ./cmd
   ```

2. **启动程序**
   ```bash
   ./smartdnssort.exe
   ```

3. **打开 Web 界面**
   ```
   http://localhost:8080
   ```

4. **启用递归**
   - 打开「配置」标签页
   - 启用「递归解析器」
   - 保存配置

5. **检查编辑器**
   - 打开「自定义设置」标签页
   - 应该能看到「Unbound Configuration」部分
   - 编辑框应该显示配置内容

### 日志检查

查看程序日志，查找与 Unbound 相关的信息：

```
[Unbound] Config file saved: unbound/unbound.conf
[Unbound] Process restarted successfully
```

### 文件检查清单

确保以下文件都已正确修改：

- [ ] `webapi/api_unbound.go` - 后端 API 处理器
- [ ] `webapi/api.go` - 路由注册（第 101 行）
- [ ] `webapi/web/components/custom-rules.html` - HTML 组件
- [ ] `webapi/web/js/modules/custom-settings.js` - JavaScript 逻辑
- [ ] `webapi/web/js/app.js` - 初始化调用（第 23 行）

### 快速测试

如果一切都正确，应该能看到：

1. **页面加载时**
   - 如果递归启用：编辑器显示，加载配置内容
   - 如果递归禁用：编辑器隐藏

2. **编辑配置时**
   - 实时显示行数和字符数
   - 支持所有标准文本编辑操作

3. **保存配置时**
   - 显示成功提示
   - Unbound 进程重启
   - 配置文件更新

### 获取帮助

如果问题仍未解决，请提供以下信息：

1. 浏览器控制台的错误信息
2. 程序日志输出
3. 网络请求的响应状态码
4. 配置文件内容（`config.yaml`）
5. 文件系统权限信息

### 相关文件

- `UNBOUND_WEB_CONFIG_FEATURE.md` - 完整功能文档
- `UNBOUND_CONFIG_QUICK_REFERENCE.md` - 快速参考
- `webapi/api_unbound.go` - 后端代码
- `webapi/web/js/modules/custom-settings.js` - 前端代码
