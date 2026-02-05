# Scripts 目录

所有脚本都应该从 `webapi/web/scripts/` 目录中运行。

## 脚本列表

### 一键安装脚本

**setup-all.bat** (Windows)
```cmd
cd webapi\web\scripts
setup-all.bat
```

**setup-all.sh** (Linux/macOS)
```bash
cd webapi/web/scripts
./setup-all.sh
```

功能：
- 安装npm依赖
- 构建Tailwind CSS
- 下载字体文件

### CSS安装脚本

**setup-css.bat** (Windows)
```cmd
cd webapi\web\scripts
setup-css.bat
```

**setup-css.sh** (Linux/macOS)
```bash
cd webapi/web/scripts
./setup-css.sh
```

功能：
- 仅安装npm依赖
- 仅构建Tailwind CSS

### CSS构建脚本

**build-css.bat** (Windows)
```cmd
cd webapi\web\scripts
build-css.bat
```

**build-css.sh** (Linux/macOS)
```bash
cd webapi/web/scripts
./build-css.sh
```

功能：
- 仅构建CSS（假设npm依赖已安装）

### 字体下载脚本

**download-fonts.py** (推荐，跨平台)
```bash
cd webapi/web/scripts
python3 download-fonts.py
```

**download-fonts.bat** (Windows)
```cmd
cd webapi\web\scripts
download-fonts.bat
```

**download-fonts.sh** (Linux/macOS)
```bash
cd webapi/web/scripts
./download-fonts.sh
```

功能：
- 下载Google Fonts到本地

## 快速开始

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

修改样式后重新构建：
```bash
cd webapi/web/scripts
./build-css.sh  # 或 build-css.bat (Windows)
```

### 下载字体

```bash
cd webapi/web/scripts
python3 download-fonts.py
```

## 脚本工作原理

所有脚本都使用相对路径来定位配置文件和字体目录：

- 脚本位置: `webapi/web/scripts/`
- 配置位置: `webapi/web/config/`
- 字体位置: `webapi/web/fonts/`
- CSS位置: `webapi/web/css/`

脚本会自动计算正确的路径，无论从哪个目录运行都能正常工作。

## 故障排除

### npm 命令不找到
- 确保已安装 Node.js
- 检查 Node.js 是否在 PATH 中

### Python 脚本失败
- 确保已安装 Python 3
- 尝试使用 `python3` 而不是 `python`
- 如果都不行，使用 `.bat` 或 `.sh` 脚本

### 字体未下载
- 检查网络连接
- 检查 `../fonts/` 目录是否存在
- 查看错误日志

## 相关文档

- [../README.md](../README.md) - 主文档
- [../STRUCTURE.md](../STRUCTURE.md) - 结构说明
- [../docs/QUICK_START.md](../docs/QUICK_START.md) - 快速开始
