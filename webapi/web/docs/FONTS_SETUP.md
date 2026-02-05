# 字体本地化设置指南

## 概述

本项目已完全移除了对Google Fonts CDN的依赖。所有字体（Spline Sans、Noto Sans、Material Symbols）现在都可以本地化部署。

## 快速开始

### 一键安装（推荐）

**Windows:**
```cmd
setup-all.bat
```

**Linux/macOS:**
```bash
chmod +x setup-all.sh
./setup-all.sh
```

这个脚本会自动：
1. 安装npm依赖
2. 构建Tailwind CSS
3. 下载所有字体文件

## 手动安装

### 步骤1: 构建CSS

```bash
cd webapi/web
npm install
npm run build
```

### 步骤2: 下载字体

选择以下任一方式：

**方式A: Python脚本（推荐，跨平台）**
```bash
python3 download-fonts.py
```

**方式B: Windows批处理**
```cmd
download-fonts.bat
```

**方式C: Linux/macOS Shell脚本**
```bash
chmod +x download-fonts.sh
./download-fonts.sh
```

## 文件说明

| 文件 | 说明 |
|------|------|
| `fonts/fonts.css` | 字体定义文件，包含所有@font-face规则 |
| `fonts/*.woff2` | 实际的字体文件（下载后生成） |
| `download-fonts.py` | Python字体下载脚本（推荐） |
| `download-fonts.bat` | Windows批处理脚本 |
| `download-fonts.sh` | Linux/macOS Shell脚本 |
| `setup-all.bat` | Windows一键安装脚本 |
| `setup-all.sh` | Linux/macOS一键安装脚本 |

## 包含的字体

### Spline Sans
- 权重: 300, 400, 500, 600, 700
- 用途: 主要UI字体
- 文件: `spline-sans-*.woff2`

### Noto Sans
- 权重: 300, 400, 500, 600, 700
- 用途: 备用字体，支持多语言
- 文件: `noto-sans-*.woff2`

### Material Symbols Outlined
- 权重: 100-700（可变字体）
- 用途: 图标字体
- 文件: `material-symbols-outlined.woff2`

## 集成到HTML

`index.html` 已自动配置为使用本地字体：

```html
<!-- Local Fonts -->
<link rel="stylesheet" href="fonts/fonts.css">

<!-- Local Built Tailwind CSS -->
<link rel="stylesheet" href="css/style.css">
```

## 性能优化

### 字体加载策略

所有字体都使用 `font-display: swap` 策略：
- 立即显示备用字体
- 字体加载完成后自动替换
- 避免文字闪烁（FOIT）

### 文件大小

典型的字体文件大小：
- Spline Sans (单个权重): ~50-80 KB
- Noto Sans (单个权重): ~60-100 KB
- Material Symbols: ~100-150 KB

总计约 1-2 MB（取决于选择的权重）

## 故障排除

### 字体未加载

1. **检查文件是否存在**
   ```bash
   ls -la fonts/*.woff2
   ```

2. **检查浏览器控制台**
   - 打开开发者工具 (F12)
   - 查看 Network 标签
   - 确认 `.woff2` 文件已加载

3. **清除缓存**
   - 强制刷新: Ctrl+F5 (Windows) 或 Cmd+Shift+R (Mac)

4. **检查CORS问题**
   - 如果从不同域名访问，确保服务器配置正确

### 下载脚本失败

**Python脚本失败:**
```bash
# 检查Python版本
python3 --version

# 尝试使用系统Python
python download-fonts.py

# 或使用其他脚本
./download-fonts.sh  # Linux/Mac
download-fonts.bat   # Windows
```

**网络问题:**
- 检查网络连接
- 尝试使用VPN
- 检查防火墙设置

## 版本控制

### 提交到Git

```bash
# 提交配置文件
git add fonts/fonts.css
git add download-fonts.*
git add setup-all.*

# 不提交字体文件（已在.gitignore中）
# 不提交node_modules（已在.gitignore中）
# 不提交生成的CSS（已在.gitignore中）
```

### 克隆后的设置

新开发者克隆项目后：

```bash
cd webapi/web
./setup-all.sh  # 或 setup-all.bat (Windows)
```

## 恢复CDN（如需要）

如果需要临时使用CDN版本，编辑 `index.html`：

```html
<!-- 注释掉本地字体 -->
<!-- <link rel="stylesheet" href="fonts/fonts.css"> -->

<!-- 添加CDN字体 -->
<link href="https://fonts.googleapis.com/css2?family=Spline+Sans:wght@300;400;500;600;700&display=swap" rel="stylesheet" />
<link href="https://fonts.googleapis.com/css2?family=Noto+Sans:wght@300;400;500;600;700&display=swap" rel="stylesheet" />
<link href="https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20..48,100..700,0..1,-50..200&display=swap" rel="stylesheet" />
```

## 技术细节

### 字体格式

使用 WOFF2 格式的原因：
- 最小的文件大小
- 所有现代浏览器支持
- 最佳的压缩率

### 字体加载流程

1. 浏览器加载 `fonts/fonts.css`
2. CSS 定义 @font-face 规则
3. 浏览器下载 `.woff2` 文件
4. 字体应用到页面元素

### 跨域资源共享 (CORS)

如果从不同域名访问字体，确保服务器返回正确的CORS头：

```
Access-Control-Allow-Origin: *
```

## 相关文档

- [CSS_BUILD_README.md](./CSS_BUILD_README.md) - Tailwind CSS 构建指南
- [tailwind.config.js](./tailwind.config.js) - Tailwind 配置
- [fonts/fonts.css](./fonts/fonts.css) - 字体定义

## 支持

如遇问题，请检查：
1. 网络连接
2. Python/Node.js 版本
3. 文件权限
4. 浏览器缓存
