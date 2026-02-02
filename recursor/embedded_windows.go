//go:build windows

package recursor

import "embed"

// Windows 平台：打包 unbound.exe 二进制文件和 root.key
// Windows 上使用嵌入的 unbound 二进制文件
//
// 打包内容：
// - binaries/windows/unbound.exe: Unbound 二进制文件
// - data/root.key: DNSSEC 根密钥
//go:embed binaries/windows/* data/root.key
var unboundBinaries embed.FS
