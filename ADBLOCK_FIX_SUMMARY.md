# AdBlock 多规则源问题修复总结

## 问题描述
用户反馈：在 Rule Sources 中配置多条规则，但是仅第一条的规则实现了，后面的规则没有实现拦截。

## 根本原因
配置文件 `config.yaml` 使用了旧的字段名：
- 旧字段：`rules` 和 `rule_file`
- 新字段：`rule_urls` 和相关的 adblock 配置

代码中 `SourceManager` 从 `cfg.RuleURLs` 读取规则源列表（source_manager.go:57），但配置文件中没有这个字段，导致没有规则源被加载。

## 已完成的修复

### 1. 更新配置文件 ✓
**文件**: `config.yaml`

将旧的 adblock 配置：
```yaml
adblock:
    allowed_ttl: 300
    block_mode: nxdomain
    blocked_response_ip: 0.0.0.0
    blocked_ttl: 3600
    enable: true
    rule_file: rules.txt
    rules:
        - rules.txt
```

更新为新的标准配置：
```yaml
adblock:
    enable: true
    engine: urlfilter
    rule_urls: []
    custom_rules_file: ./adblock_cache/custom_rules.txt
    cache_dir: ./adblock_cache
    update_interval_hours: 24
    max_cache_age_hours: 168
    max_cache_size_mb: 300
    block_mode: nxdomain
    blocked_response_ip: 0.0.0.0
    blocked_ttl: 3600
```

### 2. 创建使用文档 ✓
**文件**: `docs/adblock_multi_sources.md`

包含：
- 问题分析
- 解决方案
- 使用方法（Web UI 和配置文件）
- 常用规则源推荐
- 技术细节说明
- 故障排查指南

### 3. 创建测试脚本 ✓
**文件**: 
- `test_adblock_sources.sh` (Linux/Mac)
- `test_adblock_sources.ps1` (Windows)

用于验证多规则源功能是否正常工作。

## 使用步骤

### 方法一：通过 Web UI（推荐）

1. 启动服务后，打开 http://localhost:8080
2. 切换到 "AdBlock" 标签页
3. 在 "Add New Source" 输入框中输入规则源 URL，例如：
   - `https://easylist.to/easylist/easylist.txt`
4. 点击 "Add Source" 按钮
5. 重复步骤 3-4 添加更多规则源
6. 点击 "Update Rules" 按钮强制更新所有规则
7. 等待更新完成后，查看 "Total Rules" 数量是否增加

### 方法二：直接编辑配置文件

1. 编辑 `config.yaml`：
```yaml
adblock:
    enable: true
    engine: urlfilter
    rule_urls:
        - https://easylist.to/easylist/easylist.txt
        - https://easylist-downloads.adblockplus.org/easylistchina.txt
        - https://anti-ad.net/easylist.txt
    # ... 其他配置保持不变 ...
```

2. 重启服务

## 验证方法

### 1. 查看规则源列表
在 Web UI 的 AdBlock 标签页中，应该能看到所有添加的规则源及其状态。

### 2. 查看总规则数
"Total Rules" 应该是所有规则源的规则总和。

### 3. 测试域名拦截
在 "Test Domain" 部分测试已知的广告域名，如 `doubleclick.net`。

### 4. 运行测试脚本
```powershell
# Windows
.\test_adblock_sources.ps1

# Linux/Mac
bash test_adblock_sources.sh
```

## 技术说明

### 代码流程
1. **配置加载** (`config/config.go`)
   - 从 `config.yaml` 读取 `adblock.rule_urls`

2. **源管理器初始化** (`adblock/source_manager.go:40-69`)
   - 遍历 `cfg.RuleURLs`，为每个 URL 创建 `SourceInfo`
   - 添加自定义规则文件

3. **规则加载** (`adblock/rule_loader.go:147-179`)
   - `LoadAllRules()` 并发从所有源加载规则
   - 合并所有规则到一个数组

4. **引擎加载** (`adblock/urlfilter.go:19-50`)
   - `LoadRules()` 将所有规则加载到 URLFilter 引擎
   - 创建统一的 DNS 过滤引擎

### 关键点
- **并发加载**: 多个规则源并发下载，提高效率
- **规则合并**: 所有规则合并到一个引擎中，统一处理
- **缓存机制**: 支持 ETag 和 Last-Modified 头，避免重复下载
- **状态跟踪**: 每个源的状态、规则数、最后更新时间都被记录

## 常见问题

### Q1: 添加规则源后没有生效？
A: 需要点击 "Update Rules" 按钮手动触发更新，或等待自动更新周期。

### Q2: 某个规则源显示 "failed" 状态？
A: 可能是网络问题或 URL 无效。检查：
- URL 是否正确
- 网络是否能访问该 URL
- 服务器日志中的错误信息

### Q3: 规则数量没有增加？
A: 检查：
- 规则源是否下载成功（查看状态）
- 缓存目录 `./adblock_cache/` 中是否有对应的 `rules_*.txt` 文件
- 服务器日志中是否有错误

### Q4: 如何删除规则源？
A: 在 Web UI 的规则源列表中，点击对应规则源的 "Delete" 按钮。

## 推荐规则源

```yaml
rule_urls:
    # 国际广告拦截
    - https://easylist.to/easylist/easylist.txt
    
    # 中文广告拦截
    - https://easylist-downloads.adblockplus.org/easylistchina.txt
    
    # AdGuard 基础过滤器
    - https://raw.githubusercontent.com/AdguardTeam/FiltersRegistry/master/filters/filter_2_Base/filter.txt
    
    # AdGuard 中文过滤器
    - https://raw.githubusercontent.com/AdguardTeam/FiltersRegistry/master/filters/filter_224_Chinese/filter.txt
    
    # Anti-AD (中文)
    - https://anti-ad.net/easylist.txt
```

## 下一步

1. 重启 SmartDNSSort 服务以应用新配置
2. 通过 Web UI 添加所需的规则源
3. 触发规则更新
4. 验证多规则源是否正常工作
5. 如有问题，查看服务日志或运行测试脚本诊断
