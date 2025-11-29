# AdBlock 多规则源使用说明

## 问题描述
用户反馈：在 Rule Sources 中配置多条规则，但是仅第一条的规则实现了，后面的规则没有实现拦截。

## 问题原因
配置文件 `config.yaml` 中使用了旧的字段名，导致规则源没有正确加载。

## 解决方案

### 1. 配置文件更新
已将 `config.yaml` 中的 adblock 配置更新为正确的格式：

```yaml
adblock:
    enable: true
    engine: urlfilter                              # 使用 urlfilter 引擎（推荐）或 simple
    rule_urls: []                                   # 规则源 URL 列表
    custom_rules_file: ./adblock_cache/custom_rules.txt
    cache_dir: ./adblock_cache                     # 规则缓存目录
    update_interval_hours: 24                      # 自动更新间隔（小时）
    max_cache_age_hours: 168                       # 最大缓存时间（小时）
    max_cache_size_mb: 300                         # 最大缓存大小（MB）
    block_mode: nxdomain                           # 拦截模式：nxdomain, refused, zero_ip
    blocked_response_ip: 0.0.0.0
    blocked_ttl: 3600
```

### 2. 如何添加多个规则源

#### 方法一：通过 Web UI 添加（推荐）
1. 打开 Web UI：http://localhost:8080
2. 切换到 "AdBlock" 标签页
3. 在 "Rule Sources" 部分，输入规则源 URL
4. 点击 "Add Source" 按钮
5. 重复步骤 3-4 添加更多规则源
6. 点击 "Update Rules" 按钮强制更新所有规则

#### 方法二：直接编辑配置文件
编辑 `config.yaml`，在 `rule_urls` 下添加多个 URL：

```yaml
adblock:
    enable: true
    engine: urlfilter
    rule_urls:
        - https://easylist.to/easylist/easylist.txt
        - https://easylist-downloads.adblockplus.org/easylistchina.txt
        - https://raw.githubusercontent.com/AdguardTeam/FiltersRegistry/master/filters/filter_2_Base/filter.txt
    # ... 其他配置 ...
```

保存后重启服务。

### 3. 常用规则源推荐

```yaml
rule_urls:
    # EasyList - 国际广告拦截
    - https://easylist.to/easylist/easylist.txt
    
    # EasyList China - 中文广告拦截
    - https://easylist-downloads.adblockplus.org/easylistchina.txt
    
    # AdGuard Base Filter - AdGuard 基础过滤器
    - https://raw.githubusercontent.com/AdguardTeam/FiltersRegistry/master/filters/filter_2_Base/filter.txt
    
    # AdGuard Chinese Filter - AdGuard 中文过滤器
    - https://raw.githubusercontent.com/AdguardTeam/FiltersRegistry/master/filters/filter_224_Chinese/filter.txt
    
    # Anti-AD - 中文广告拦截
    - https://anti-ad.net/easylist.txt
```

### 4. 验证多规则源是否生效

1. **查看规则源列表**
   - 在 Web UI 的 AdBlock 标签页中，查看 "Rule Sources" 表格
   - 应该能看到所有添加的规则源及其状态

2. **查看总规则数**
   - 在 "AdBlock Status" 部分查看 "Total Rules"
   - 数量应该是所有规则源的规则总和

3. **测试域名拦截**
   - 在 "Test Domain" 部分输入一个已知的广告域名
   - 点击 "Test" 按钮
   - 如果被拦截，会显示匹配的规则

### 5. 技术细节

#### 规则加载流程
1. `SourceManager` 管理所有规则源
2. `RuleLoader.LoadAllRules()` 从所有源并发加载规则
3. 所有规则合并到一个数组中
4. `URLFilterEngine.LoadRules()` 将合并后的规则加载到过滤引擎

#### 关键代码位置
- 规则源管理：`adblock/source_manager.go`
- 规则加载：`adblock/rule_loader.go`
- 过滤引擎：`adblock/urlfilter.go`
- Web API：`webapi/api.go`

### 6. 故障排查

如果多规则源仍然不生效，请检查：

1. **查看日志**
   ```bash
   # 查看服务日志，检查是否有错误信息
   ```

2. **检查规则源状态**
   - 在 Web UI 中查看每个规则源的状态
   - 状态应该是 "active"
   - 如果是 "failed" 或 "bad"，说明该源下载失败

3. **检查缓存目录**
   ```bash
   ls -la ./adblock_cache/
   ```
   应该能看到：
   - `rules_meta.json` - 规则源元数据
   - `rules_*.txt` - 各个规则源的缓存文件
   - `custom_rules.txt` - 自定义规则文件

4. **手动触发更新**
   - 在 Web UI 点击 "Update Rules" 按钮
   - 等待更新完成后刷新页面

5. **重启服务**
   ```bash
   # 重启 SmartDNSSort 服务
   ```

### 7. 注意事项

1. **规则更新时间**
   - 规则会根据 `update_interval_hours` 自动更新
   - 也可以通过 Web UI 手动触发更新

2. **内存占用**
   - 多个规则源会增加内存占用
   - 可以通过 `max_cache_size_mb` 限制缓存大小

3. **下载超时**
   - 规则源下载有 15 秒超时限制
   - 如果网络较慢，某些源可能下载失败

4. **规则冲突**
   - 如果多个源有冲突的规则（如白名单和黑名单），以最后匹配的为准
   - URLFilter 引擎会自动处理规则优先级
