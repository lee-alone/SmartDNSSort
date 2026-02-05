# 脚本路径修复说明

**修复日期**: 2026-02-05  
**问题**: 脚本在 `scripts/` 目录中执行时，npm 找不到 `package.json`  
**状态**: ✅ 已修复

---

## 问题描述

原始脚本使用相对路径 `cd config` 来进入配置目录，但当脚本从 `scripts/` 目录运行时，这会导致进入 `scripts/config` 而不是 `webapi/web/config`。

```
错误: npm error path F:\gb\SmartDNSSort\webapi\web\scripts\package.json
```

---

## 解决方案

所有脚本已更新为使用绝对路径计算：

### Windows (Batch)
```batch
set SCRIPT_DIR=%~dp0
set WEB_DIR=%SCRIPT_DIR%..
set CONFIG_DIR=%WEB_DIR%\config

pushd "%CONFIG_DIR%"
npm install
popd
```

### Linux/macOS (Bash)
```bash
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$(dirname "$SCRIPT_DIR")"
CONFIG_DIR="$WEB_DIR/config"

cd "$CONFIG_DIR"
npm install
cd "$WEB_DIR"
```

---

## 已修复的脚本

| 脚本 | 修复内容 |
|------|--------|
| setup-all.bat | 使用 pushd/popd 和绝对路径 |
| setup-all.sh | 使用 SCRIPT_DIR 计算绝对路径 |
| setup-css.bat | 使用 pushd/popd 和绝对路径 |
| setup-css.sh | 使用 SCRIPT_DIR 计算绝对路径 |
| build-css.bat | 使用 pushd/popd 和绝对路径 |
| build-css.sh | 使用 SCRIPT_DIR 计算绝对路径 |
| download-fonts.py | 更新 FONTS_DIR 为 `../fonts` |
| download-fonts.bat | 更新 FONTS_DIR 为 `..\fonts` |
| download-fonts.sh | 更新 FONTS_DIR 为 `../fonts` |

---

## 使用方式

### 从 scripts/ 目录运行

**Windows:**
```cmd
cd webapi\web\scripts
setup-all.bat
```

**Linux/macOS:**
```bash
cd webapi/web/scripts
./setup-all.sh
```

### 从 webapi/web/ 目录运行

**Windows:**
```cmd
cd webapi\web
scripts\setup-all.bat
```

**Linux/macOS:**
```bash
cd webapi/web
./scripts/setup-all.sh
```

两种方式都能正常工作！

---

## 技术细节

### Windows 路径计算

```batch
%~dp0          # 脚本所在目录 (webapi/web/scripts/)
%SCRIPT_DIR%.. # 上一级目录 (webapi/web/)
```

### Linux/macOS 路径计算

```bash
${BASH_SOURCE[0]}     # 脚本文件路径
dirname               # 获取目录
cd "$(dirname ...)"   # 进入脚本目录
pwd                   # 获取绝对路径
```

---

## 验证

运行脚本后，应该看到：

```
[INFO] Installing npm dependencies...
[SUCCESS] CSS setup complete!
[INFO] Downloading fonts...
✓ Font download complete!
[SUCCESS] Complete setup finished!
```

如果看到这些消息，说明脚本已正确执行。

---

## 相关文件

- [scripts/README.md](./scripts/README.md) - 脚本使用说明
- [README.md](./README.md) - 主文档
- [STRUCTURE.md](./STRUCTURE.md) - 结构说明

---

**版本**: 1.0.0  
**状态**: ✅ 完成
