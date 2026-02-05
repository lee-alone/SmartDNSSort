# SmartDNSSort Web UI

现代化的Web界面，完全本地化部署，无CDN依赖。

## 📁 目录结构

```
webapi/web/
├── index.html                   # 主入口文件
├── favicon.svg                  # 网站图标
│
├── css/                         # 样式文件
│   ├── input.css               # Tailwind CSS源文件
│   └── style.css               # 生成的CSS（自动生成）
│
├── fonts/                       # 字体文件
│   ├── fonts.css               # 字体定义
│   └── *.woff2                 # 字体文件（下载后生成）
│
├── js/                         # JavaScript模块
│   ├── app.js                  # 主应用
│   ├── i18n/                   # 国际化
│   └── modules/                # 功能模块
│
├── components/                 # HTML组件
│   ├── dashboard.html
│   ├── config.html
│   └── ...
│
├── config/                     # 配置文件
│   ├── package.json            # npm配置
│   ├── tailwind.config.js      # Tailwind配置
│   └── postcss.config.js       # PostCSS配置
│
├── scripts/                    # 构建和安装脚本
│   ├── setup-all.bat/sh        # 一键安装脚本
│   ├── setup-css.bat/sh        # CSS安装脚本
│   ├── build-css.bat/sh        # CSS构建脚本
│   └── download-fonts.*        # 字体下载脚本
│
├── docs/                       # 文档
│   ├── README_FONTS_AND_CSS.md # 总览
│   ├── QUICK_START.md          # 快速开始
│   ├── FONTS_SETUP.md          # 字体设置
│   ├── CSS_BUILD_README.md     # CSS构建
│   ├── MIGRATION_SUMMARY.md    # 迁移总结
│   ├── IMPLEMENTATION_CHECKLIST.md
│   └── REVIEW_REPORT.md        # 审核报告
│
└── backup/                     # 备份文件
    ├── index.html.bak
    ├── index_old.html.bak
    └── quick-test.html
```

## 🚀 快速开始

### 首次安装

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

### 日常开发

```bash
# 修改样式后重新构建
cd config
npm run build
cd ..

# 开发模式（自动监听）
cd config
npm run watch
cd ..

# 下载字体
python3 scripts/download-fonts.py
```

## 📚 文档

所有文档都在 `docs/` 目录中：

| 文档 | 用途 |
|------|------|
| [README_FONTS_AND_CSS.md](./docs/README_FONTS_AND_CSS.md) | 总览和导航 |
| [QUICK_START.md](./docs/QUICK_START.md) | 5分钟快速开始 |
| [FONTS_SETUP.md](./docs/FONTS_SETUP.md) | 详细字体指南 |
| [CSS_BUILD_README.md](./docs/CSS_BUILD_README.md) | CSS构建指南 |
| [MIGRATION_SUMMARY.md](./docs/MIGRATION_SUMMARY.md) | 迁移过程总结 |
| [IMPLEMENTATION_CHECKLIST.md](./docs/IMPLEMENTATION_CHECKLIST.md) | 实现检查清单 |
| [REVIEW_REPORT.md](./docs/REVIEW_REPORT.md) | 完整审核报告 |

## 🛠️ 脚本

所有脚本都在 `scripts/` 目录中：

| 脚本 | 用途 |
|------|------|
| `setup-all.bat/sh` | 一键安装（推荐） |
| `setup-css.bat/sh` | 仅安装CSS |
| `build-css.bat/sh` | 仅构建CSS |
| `download-fonts.py` | 下载字体（推荐） |
| `download-fonts.bat/sh` | 下载字体（备选） |

## ⚙️ 配置

所有配置文件都在 `config/` 目录中：

| 文件 | 说明 |
|------|------|
| `package.json` | npm依赖和脚本 |
| `tailwind.config.js` | Tailwind CSS配置 |
| `postcss.config.js` | PostCSS配置 |

## 📦 包含的字体

- **Spline Sans** - 主UI字体（权重: 300-700）
- **Noto Sans** - 备用字体，支持多语言（权重: 300-700）
- **Material Symbols** - 图标字体

## ✨ 特性

- ✅ 无CDN依赖 - 完全本地化
- ✅ 高性能 - 本地文件加载
- ✅ 离线支持 - 无需网络连接
- ✅ 完全可定制 - 控制所有样式
- ✅ 跨平台 - Windows/Linux/macOS
- ✅ 多语言 - 支持国际化

## 🌐 浏览器支持

| 浏览器 | 支持 |
|--------|------|
| Chrome/Edge | ✅ |
| Firefox | ✅ |
| Safari | ✅ |
| IE 11 | ⚠️ |

## 📊 文件大小

| 组件 | 大小 |
|------|------|
| Spline Sans (5个权重) | ~300-400 KB |
| Noto Sans (5个权重) | ~300-500 KB |
| Material Symbols | ~100-150 KB |
| Tailwind CSS | ~50-100 KB |
| **总计** | **~1-2 MB** |

## 🐛 故障排除

### 样式不生效
```bash
cd config
npm run build
cd ..
# 清除浏览器缓存 (Ctrl+F5)
```

### 字体未加载
```bash
python3 scripts/download-fonts.py
# 检查 fonts/ 目录中是否有 .woff2 文件
```

### npm 命令不找到
重新安装 Node.js: https://nodejs.org/

## 📞 支持

遇到问题？请查看 `docs/` 目录中的相关文档。

---

**版本**: 1.0.0  
**最后更新**: 2026-02-05  
**状态**: ✅ 生产就绪
