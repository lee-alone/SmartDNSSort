//go:build linux

package recursor

import "embed"

// Linux 平台：仅打包数据文件，不打包 unbound 二进制文件
// Linux 上使用系统安装的 unbound，无需打包二进制文件
//
// 打包内容：
// - data/root.key: DNSSEC 根密钥（用于 fallback）
// - data/root.zone: 根域 zone 文件
//
// 不打包内容：
// - binaries/linux/unbound: 使用系统安装的 unbound
// - data/unbound.conf: 配置动态生成，无需打包
//go:embed data/root.key data/root.zone
var unboundBinaries embed.FS
