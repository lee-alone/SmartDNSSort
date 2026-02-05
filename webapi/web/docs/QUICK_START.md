# 快速开始指南

## 首次设置（5分钟）

### Windows
```cmd
cd webapi\web
setup-all.bat
```

### Linux/macOS
```bash
cd webapi/web
chmod +x setup-all.sh
./setup-all.sh
```

完成！所有CSS和字体已准备好。

## 日常开发

### 修改样式后重新构建
```bash
cd webapi/web
npm run build
```

### 开发模式（自动监听）
```bash
cd webapi/web
npm run watch
```

### 修改字体后重新下载
```bash
cd webapi/web
python3 download-fonts.py
```

## 文件位置

| 文件 | 位置 | 说明 |
|------|------|------|
| 样式输入 | `css/input.css` | 编辑自定义样式 |
| 样式输出 | `css/style.css` | 生成的CSS（勿编辑） |
| 字体定义 | `fonts/fonts.css` | 字体@font-face规则 |
| 字体文件 | `fonts/*.woff2` | 实际字体文件 |
| Tailwind配置 | `tailwind.config.js` | 颜色、字体等配置 |

## 常见任务

### 添加新颜色
编辑 `tailwind.config.js` 的 `colors` 部分，然后运行 `npm run build`

### 添加新字体
1. 编辑 `fonts/fonts.css` 添加 @font-face
2. 运行 `python3 download-fonts.py` 下载字体文件
3. 在 `tailwind.config.js` 中配置字体

### 修改主题
编辑 `tailwind.config.js` 中的 `theme.extend` 部分

## 检查清单

- [ ] Node.js 已安装 (`node --version`)
- [ ] npm 依赖已安装 (`npm install`)
- [ ] CSS 已构建 (`npm run build`)
- [ ] 字体已下载 (`python3 download-fonts.py`)
- [ ] 浏览器缓存已清除 (Ctrl+F5)

## 故障排除

| 问题 | 解决方案 |
|------|--------|
| 样式不生效 | 运行 `npm run build` 并清除浏览器缓存 |
| 字体未加载 | 检查 `fonts/` 目录中是否有 `.woff2` 文件 |
| npm 命令不找到 | 重新安装 Node.js |
| Python 脚本失败 | 尝试 `download-fonts.bat` (Windows) 或 `download-fonts.sh` (Linux/Mac) |

## 更多信息

- [CSS_BUILD_README.md](./CSS_BUILD_README.md) - 详细的CSS构建指南
- [FONTS_SETUP.md](./FONTS_SETUP.md) - 详细的字体设置指南
