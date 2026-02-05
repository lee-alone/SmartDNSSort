# Web文件夹结构说明

## 📋 快速参考

```
webapi/web/
├── 📄 index.html              ← 主入口
├── 📄 README.md               ← 开始这里
├── 📄 STRUCTURE.md            ← 本文件
│
├── 📁 css/                    ← 样式文件
│   ├── input.css              ← 编辑这个
│   └── style.css              ← 自动生成
│
├── 📁 fonts/                  ← 字体文件
│   ├── fonts.css              ← 字体定义
│   └── *.woff2                ← 字体文件
│
├── 📁 js/                     ← JavaScript
│   ├── app.js
│   ├── i18n/
│   └── modules/
│
├── 📁 components/             ← HTML组件
│   ├── dashboard.html
│   ├── config.html
│   └── ...
│
├── 📁 config/                 ← 配置文件
│   ├── package.json           ← npm配置
│   ├── tailwind.config.js     ← Tailwind配置
│   └── postcss.config.js      ← PostCSS配置
│
├── 📁 scripts/                ← 脚本
│   ├── setup-all.bat/sh       ← 一键安装
│   ├── setup-css.bat/sh       ← CSS安装
│   ├── build-css.bat/sh       ← CSS构建
│   └── download-fonts.*       ← 字体下载
│
├── 📁 docs/                   ← 文档
│   ├── README_FONTS_AND_CSS.md
│   ├── QUICK_START.md
│   ├── FONTS_SETUP.md
│   ├── CSS_BUILD_README.md
│   ├── MIGRATION_SUMMARY.md
│   ├── IMPLEMENTATION_CHECKLIST.md
│   └── REVIEW_REPORT.md
│
└── 📁 backup/                 ← 备份文件
    ├── index.html.bak
    ├── index_old.html.bak
    └── quick-test.html
```

## 🎯 常见任务

### 修改样式
1. 编辑 `css/input.css`
2. 运行 `cd config && npm run build && cd ..`
3. 刷新浏览器

### 修改配置
1. 编辑 `config/tailwind.config.js`
2. 运行 `cd config && npm run build && cd ..`

### 下载字体
```bash
python3 scripts/download-fonts.py
```

### 开发模式
```bash
cd config
npm run watch
cd ..
```

## 📂 目录说明

### css/
- **input.css** - Tailwind CSS源文件，包含自定义样式
- **style.css** - 生成的CSS文件（勿手动编辑）

### fonts/
- **fonts.css** - 字体定义文件
- **\*.woff2** - 实际的字体文件（下载后生成）

### js/
- **app.js** - 主应用入口
- **i18n/** - 国际化模块
- **modules/** - 功能模块（dashboard、config等）

### components/
- HTML组件文件
- 由JavaScript动态加载

### config/
- **package.json** - npm依赖和脚本
- **tailwind.config.js** - Tailwind CSS配置
- **postcss.config.js** - PostCSS配置

### scripts/
- **setup-all.bat/sh** - 完整安装脚本
- **setup-css.bat/sh** - CSS安装脚本
- **build-css.bat/sh** - CSS构建脚本
- **download-fonts.py/bat/sh** - 字体下载脚本

### docs/
- 所有文档文件
- 按用途分类

### backup/
- 旧版本文件
- 测试文件

## 🔄 工作流程

### 首次设置
```bash
cd webapi/web/scripts
./setup-all.bat  # Windows
./setup-all.sh   # Linux/macOS
```

### 日常开发
```bash
# 修改样式
cd config
npm run build
cd ..

# 或开发模式
cd config
npm run watch
cd ..
```

### 修改字体
```bash
python3 scripts/download-fonts.py
```

## 📖 文档导航

| 需求 | 文档 |
|------|------|
| 总览 | [README.md](./README.md) |
| 快速开始 | [docs/QUICK_START.md](./docs/QUICK_START.md) |
| 字体设置 | [docs/FONTS_SETUP.md](./docs/FONTS_SETUP.md) |
| CSS构建 | [docs/CSS_BUILD_README.md](./docs/CSS_BUILD_README.md) |
| 完整指南 | [docs/README_FONTS_AND_CSS.md](./docs/README_FONTS_AND_CSS.md) |

## ✅ 检查清单

- [ ] 已运行 `scripts/setup-all.bat` 或 `./scripts/setup-all.sh`
- [ ] 已下载字体文件
- [ ] 已构建CSS
- [ ] 已清除浏览器缓存
- [ ] 页面样式正确显示

## 🚀 下一步

1. 查看 [README.md](./README.md)
2. 运行 `scripts/setup-all.bat` 或 `./scripts/setup-all.sh`
3. 查看 [docs/QUICK_START.md](./docs/QUICK_START.md)
4. 开始开发！

---

**版本**: 1.0.0  
**最后更新**: 2026-02-05
