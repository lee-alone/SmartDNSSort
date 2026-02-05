# Tailwind CSS 本地化指南

## 概述

本项目已经配置好了将 Tailwind CSS 从 CDN 迁移到本地构建系统。这样可以避免生产环境警告，提高页面加载速度，并消除网络依赖带来的潜在问题。

## 文件结构

```
webapi/web/
├── package.json              # Node.js 依赖包配置
├── tailwind.config.js        # Tailwind CSS 配置
├── postcss.config.js         # PostCSS 配置
├── css/
│   ├── input.css            # CSS 输入文件（包含自定义样式）
│   └── style.css            # 编译后的 CSS 文件
├── fonts/
│   ├── fonts.css            # 字体定义文件
│   ├── spline-sans-*.woff2  # Spline Sans 字体文件
│   ├── noto-sans-*.woff2    # Noto Sans 字体文件
│   └── material-symbols-outlined.woff2  # Material Symbols 字体
├── setup-css.bat            # Windows CSS 安装脚本
├── setup-css.sh             # Unix/Linux CSS 安装脚本
├── download-fonts.bat       # Windows 字体下载脚本
├── download-fonts.sh        # Unix/Linux 字体下载脚本
├── download-fonts.py        # Python 字体下载脚本（推荐）
└── index.html               # 已更新为使用本地 CSS 和字体
```

## 首次安装

### Windows 系统

1. 确保已安装 Node.js（从 https://nodejs.org/ 下载安装）
2. 在 `webapi/web/` 目录下运行：
   ```cmd
   setup-css.bat
   ```
3. 下载字体文件（选择以下任一方式）：
   ```cmd
   REM 方式1: 使用Python脚本（推荐）
   python download-fonts.py
   
   REM 方式2: 使用批处理脚本
   download-fonts.bat
   ```

### Unix/Linux/macOS 系统

1. 确保已安装 Node.js
2. 在 `webapi/web/` 目录下运行：
   ```bash
   chmod +x setup-css.sh
   ./setup-css.sh
   ```
3. 下载字体文件：
   ```bash
   # 方式1: 使用Python脚本（推荐）
   python3 download-fonts.py
   
   # 方式2: 使用Shell脚本
   chmod +x download-fonts.sh
   ./download-fonts.sh
   ```

### 手动安装（任何系统）

1. 进入 `webapi/web/` 目录
2. 安装依赖：
   ```bash
   npm install
   ```
3. 构建样式：
   ```bash
   npm run build
   ```
4. 下载字体文件：
   ```bash
   python3 download-fonts.py
   ```

## 日常使用

### 修改样式后重新构建

```bash
cd webapi/web
npm run build
```

### 开发模式（自动监听文件变化）

在开发过程中，可以使用 watch 模式，文件修改后会自动重新构建：

```bash
cd webapi/web
npm run watch
```

## 配置说明

### Tailwind 配置 (`tailwind.config.js`)

包含您项目的自定义配置：
- 颜色主题（浅色和深色模式）
- 自定义字体
- 圆角设置
- forms 和 container-queries 插件

### PostCSS 配置 (`postcss.config.js`)

标准的 PostCSS 配置，支持 Tailwind CSS 和 Autoprefixer。

### 输入文件 (`css/input.css`)

包含：
- Tailwind 指令 (`@tailwind base`, `@tailwind components`, `@tailwind utilities`)
- 您所有的自定义 CSS 样式

## 添加新样式

1. 在 HTML 中使用 Tailwind 类名，或者在 `css/input.css` 中添加自定义 CSS
2. 运行 `npm run build` 重新构建

## 修改现有样式

1. 在 `tailwind.config.js` 中修改配置
2. 在 `css/input.css` 中修改自定义样式
3. 运行 `npm run build` 重新构建

## 优势

✅ **无 CDN 依赖** - 消除网络加载问题  
✅ **无生产环境警告** - 使用官方推荐的构建方式  
✅ **更快的加载速度** - CSS 和字体都是本地文件  
✅ **更好的性能** - 编译后的 CSS 更小且优化，字体可预加载  
✅ **版本控制** - 可将生成的 CSS 提交到版本库  
✅ **离线使用** - 无需网络连接即可使用完整功能  

## 备份说明

原始的 HTML 文件已备份为 `index_old.html.bak`。如需恢复，请：

```bash
# 恢复原始文件
mv index_old.html.bak index.html
```

## 故障排除

### 常见问题

**Q: 运行构建时提示找不到 npx 命令**  
A: 确保已正确安装 Node.js，并将其添加到系统 PATH 中。

**Q: 构建成功但页面样式不生效**  
A: 检查浏览器缓存，尝试强制刷新（Ctrl+F5）。

**Q: 修改配置后样式没有变化**  
A: 需要重新运行 `npm run build` 来重新生成 CSS 文件。

**Q: 字体下载失败**  
A: 
- 检查网络连接
- 尝试使用 Python 脚本：`python3 download-fonts.py`
- 如果 Python 不可用，尝试批处理脚本（Windows）或 Shell 脚本（Linux/Mac）
- 确保 `fonts/` 目录存在且可写

**Q: 字体文件下载后页面仍无字体**  
A:
- 清除浏览器缓存（Ctrl+F5）
- 检查浏览器开发者工具的 Network 标签，确认字体文件已加载
- 确保 `fonts/fonts.css` 被正确引入到 `index.html`

### 恢复 CDN

如果需要恢复使用 CDN，将 `index.html` 中的：

```html
<!-- Local Fonts -->
<link rel="stylesheet" href="fonts/fonts.css">

<!-- Local Built Tailwind CSS -->
<link rel="stylesheet" href="css/style.css">
```

替换为原始的 CDN 引用即可。

## 技术支持

- Tailwind CSS 官方文档：https://tailwindcss.com/docs
- Node.js 官方网站：https://nodejs.org/