# Web UI - 字体和CSS本地化

## 📌 快速导航

| 需求 | 文档 |
|------|------|
| 🚀 **我想快速开始** | [QUICK_START.md](./QUICK_START.md) |
| 📖 **我想了解详细步骤** | [FONTS_SETUP.md](./FONTS_SETUP.md) |
| 🔧 **我想了解CSS构建** | [CSS_BUILD_README.md](./CSS_BUILD_README.md) |
| 📋 **我想查看完整清单** | [IMPLEMENTATION_CHECKLIST.md](./IMPLEMENTATION_CHECKLIST.md) |
| 📝 **我想了解迁移过程** | [MIGRATION_SUMMARY.md](./MIGRATION_SUMMARY.md) |

## 🎯 一句话总结

本项目已完全移除CDN依赖，所有CSS和字体都本地化部署。

## ⚡ 30秒快速开始

### Windows
```cmd
cd webapi\web
setup-all.bat
```

### Linux/macOS
```bash
cd webapi/web
./setup-all.sh
```

完成！所有CSS和字体已准备好。

## 📦 包含内容

### 字体
- **Spline Sans** - 主UI字体（权重: 300-700）
- **Noto Sans** - 备用字体，支持多语言（权重: 300-700）
- **Material Symbols** - 图标字体

### 样式
- **Tailwind CSS** - 本地构建，完全自定义
- **自定义CSS** - 状态指示器、配置样式等

## 🗂️ 文件结构

```
webapi/web/
├── fonts/                           # 字体目录
│   ├── fonts.css                   # 字体定义
│   └── *.woff2                     # 字体文件（下载后生成）
├── css/                            # 样式目录
│   ├── input.css                   # 源文件
│   └── style.css                   # 生成的CSS
├── setup-all.bat/sh                # 一键安装脚本
├── download-fonts.*                # 字体下载脚本
└── *.md                            # 文档
```

## 🔄 工作流程

### 首次设置
```bash
setup-all.bat  # 或 setup-all.sh
```

### 修改样式
```bash
npm run build
```

### 开发模式（自动监听）
```bash
npm run watch
```

### 修改字体
```bash
python3 download-fonts.py
```

## ✨ 主要特性

| 特性 | 说明 |
|------|------|
| 🚀 **高性能** | 本地文件，无CDN延迟 |
| 🔒 **可靠** | 无网络依赖，离线可用 |
| 📦 **完整** | 所有资源都在项目中 |
| 🎨 **可定制** | 完全控制样式和字体 |
| 📝 **版本控制** | 配置文件可提交到Git |
| 🌍 **多语言** | Noto Sans支持多语言 |

## 🛠️ 技术栈

- **Tailwind CSS** v3.4.0 - 实用优先的CSS框架
- **PostCSS** v8.4.0 - CSS转换工具
- **Autoprefixer** v10.4.0 - 浏览器前缀自动添加
- **WOFF2** - 最优的字体格式

## 📊 文件大小

| 项目 | 大小 |
|------|------|
| Spline Sans (5个权重) | ~300-400 KB |
| Noto Sans (5个权重) | ~300-500 KB |
| Material Symbols | ~100-150 KB |
| Tailwind CSS | ~50-100 KB |
| **总计** | **~1-2 MB** |

## 🌐 浏览器支持

| 浏览器 | 支持 |
|--------|------|
| Chrome/Edge | ✅ 完全支持 |
| Firefox | ✅ 完全支持 |
| Safari | ✅ 完全支持 |
| IE 11 | ⚠️ 需要备用方案 |

## 🐛 故障排除

### 样式不生效
```bash
npm run build
# 清除浏览器缓存 (Ctrl+F5)
```

### 字体未加载
```bash
python3 download-fonts.py
# 检查 fonts/ 目录中是否有 .woff2 文件
```

### npm 命令不找到
```bash
# 重新安装 Node.js
# https://nodejs.org/
```

## 📚 详细文档

- **[QUICK_START.md](./QUICK_START.md)** - 5分钟快速开始
- **[FONTS_SETUP.md](./FONTS_SETUP.md)** - 字体设置详解
- **[CSS_BUILD_README.md](./CSS_BUILD_README.md)** - CSS构建详解
- **[MIGRATION_SUMMARY.md](./MIGRATION_SUMMARY.md)** - 迁移过程总结
- **[IMPLEMENTATION_CHECKLIST.md](./IMPLEMENTATION_CHECKLIST.md)** - 实现检查清单

## 🎓 学习资源

- [Tailwind CSS 官方文档](https://tailwindcss.com/docs)
- [Google Fonts](https://fonts.google.com/)
- [Material Symbols](https://fonts.google.com/icons)
- [WOFF2 格式](https://www.w3.org/TR/WOFF2/)

## 💡 常见问题

**Q: 为什么要本地化？**  
A: 提高性能、可靠性和隐私保护，消除CDN依赖。

**Q: 字体文件很大吗？**  
A: 总共约1-2MB，使用WOFF2格式已是最优压缩。

**Q: 可以只下载部分字体吗？**  
A: 可以，编辑 `download-fonts.py` 或相关脚本。

**Q: 如何添加新字体？**  
A: 编辑 `fonts/fonts.css` 添加@font-face，然后下载字体文件。

**Q: 支持离线使用吗？**  
A: 是的，所有资源都是本地的，完全支持离线使用。

## 🚀 下一步

1. 运行 `setup-all.bat` 或 `setup-all.sh`
2. 查看 [QUICK_START.md](./QUICK_START.md)
3. 开始开发！

## 📞 支持

遇到问题？请检查：
1. 相关文档中的故障排除部分
2. 浏览器开发者工具（F12）
3. 确保所有依赖都已安装

---

**版本**: 1.0.0  
**最后更新**: 2026-02-05  
**状态**: ✅ 生产就绪
