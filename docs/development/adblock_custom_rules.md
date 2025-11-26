# AdBlock 自定义规则文件自动创建功能

## 概述

在配置文件 `config.yaml` 中，可以通过 `adblock.custom_rules_file` 字段指定用户自定义的广告屏蔽规则文件路径。现在，如果该文件不存在，程序会**自动创建**它。

## 功能特点

1. **自动创建**：当程序启动时，如果指定的自定义规则文件不存在，会自动创建该文件
2. **包含使用说明**：自动创建的文件包含详细的中文使用说明和示例规则
3. **不覆盖现有文件**：如果文件已经存在，程序不会覆盖其内容
4. **自动创建目录**：如果父目录不存在，也会自动创建

## 配置示例

在 `config.yaml` 中：

```yaml
adblock:
  enable: true
  engine: urlfilter
  rule_urls:
    - https://easylist.to/easylist/easylist.txt
  custom_rules_file: ./adblock_cache/custom_rules.txt  # 自定义规则文件路径
  cache_dir: ./adblock_cache
  update_interval_hours: 24
  max_cache_age_hours: 168
  max_cache_size_mb: 300
  block_mode: nxdomain
  blocked_ttl: 3600
```

## 自动创建的文件内容

当文件不存在时，程序会创建一个包含以下内容的文件：

```txt
# SmartDNSSort 自定义广告屏蔽规则文件
# 
# 在此文件中添加您自己的广告屏蔽规则
# 每行一条规则，支持以下格式：
#
# 1. 域名匹配（推荐）：
#    ||example.com^         - 屏蔽 example.com 及其所有子域名
#    ||ads.example.com^     - 仅屏蔽 ads.example.com
#
# 2. 通配符匹配：
#    *ads.*                 - 屏蔽包含 'ads.' 的所有域名
#
# 3. 正则表达式（高级）：
#    /^ad[s]?\./            - 使用正则表达式匹配
#
# 以 # 开头的行为注释，将被忽略
# 空行也会被忽略
#
# 示例规则（取消注释以启用）：
# ||doubleclick.net^
# ||googleadservices.com^
# ||googlesyndication.com^
# ||advertising.com^

```

## 使用方法

1. **启动程序**：首次运行程序时，自定义规则文件会自动创建
2. **编辑规则**：打开 `./adblock_cache/custom_rules.txt` 文件
3. **添加规则**：按照文件中的说明添加您自己的屏蔽规则
4. **重新加载**：程序会在下次更新规则时自动加载您的自定义规则

## 规则格式说明

### 1. 域名匹配（推荐）

```txt
||example.com^
```
- 这会屏蔽 `example.com` 及其所有子域名
- 例如：`www.example.com`、`api.example.com` 都会被屏蔽

### 2. 通配符匹配

```txt
*ads.*
```
- 这会屏蔽所有包含 `ads.` 的域名
- 例如：`ads.example.com`、`static.ads.example.com` 都会被屏蔽

### 3. 正则表达式

```txt
/^ad[s]?\./
```
- 使用正则表达式进行更复杂的匹配
- 适合高级用户使用

## 技术实现

实现位于 `adblock/source_manager.go` 文件中：

### 核心函数

```go
func (sm *SourceManager) ensureCustomRulesFile(filePath string) error {
    // 检查文件是否已存在
    if _, err := os.Stat(filePath); err == nil {
        return nil // 文件存在，无需操作
    } else if !os.IsNotExist(err) {
        return err // 其他错误
    }

    // 确保父目录存在
    dir := filepath.Dir(filePath)
    if err := os.MkdirAll(dir, 0755); err != nil {
        return err
    }

    // 创建带有说明的文件
    return os.WriteFile(filePath, []byte(defaultContent), 0644)
}
```

### 调用时机

在 `NewSourceManager` 函数中，会在添加自定义规则源之前自动调用：

```go
if cfg.CustomRulesFile != "" {
    // 创建自定义规则文件（如果不存在）
    if err := sm.ensureCustomRulesFile(cfg.CustomRulesFile); err != nil {
        return nil, err
    }
    sm.AddSource(cfg.CustomRulesFile)
}
```

## 测试

已添加单元测试以验证功能：

- `TestEnsureCustomRulesFile`：测试文件不存在时自动创建
- `TestEnsureCustomRulesFileAlreadyExists`：测试文件已存在时不覆盖

运行测试：

```bash
go test -v ./adblock -run TestEnsureCustomRulesFile
```

## 兼容性

此功能向后兼容，不会影响现有的配置和使用方式。
