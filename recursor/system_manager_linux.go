//go:build linux

package recursor

import (
	"fmt"
	"os"
	"os/exec"
	"smartdnssort/logger"
	"strings"
)

// ensureRootKeyLinux Linux 特定的 root.key 管理
// 优先使用 unbound-anchor 生成，失败时使用嵌入的 root.key
func (sm *SystemManager) ensureRootKeyLinux() (string, error) {
	rootKeyPath := "/etc/unbound/root.key"

	// 1. 如果文件已存在且有效，直接返回
	if info, err := os.Stat(rootKeyPath); err == nil && info.Size() > 1024 {
		logger.Infof("[SystemManager] Using existing root.key: %s", rootKeyPath)
		return rootKeyPath, nil
	}

	// 2. 确保目录存在
	if err := os.MkdirAll("/etc/unbound", 0755); err != nil {
		return "", fmt.Errorf("failed to create /etc/unbound: %w", err)
	}

	// 3. 尝试使用 unbound-anchor 生成
	logger.Infof("[SystemManager] Attempting to generate root.key using unbound-anchor...")
	if err := sm.runUnboundAnchor(rootKeyPath); err == nil {
		logger.Infof("[SystemManager] Root key generated successfully")
		return rootKeyPath, nil
	}

	// 4. Fallback 到嵌入的 root.key
	logger.Warnf("[SystemManager] unbound-anchor failed, using embedded root.key")
	if err := sm.extractEmbeddedRootKey(rootKeyPath); err != nil {
		return "", fmt.Errorf("both unbound-anchor and embedded root.key failed: %w", err)
	}

	logger.Infof("[SystemManager] Using embedded root.key as fallback")
	return rootKeyPath, nil
}

// runUnboundAnchor 运行 unbound-anchor 命令生成 root.key
func (sm *SystemManager) runUnboundAnchor(rootKeyPath string) error {
	// unbound-anchor 参数：
	// -a <path>: 指定 root.key 输出路径
	// -4: 强制使用 IPv4（可选，在首次启动时很重要）

	cmd := exec.Command("unbound-anchor", "-a", rootKeyPath, "-4")
	output, err := cmd.CombinedOutput()

	if err != nil {
		logger.Debugf("[SystemManager] unbound-anchor output: %s", string(output))

		// 检查是否是临时性错误（可以 fallback）
		if sm.isTemporaryAnchorError(err, string(output)) {
			return err // 返回错误，让调用者使用 fallback
		}

		// 严重错误，不应该 fallback
		return fmt.Errorf("unbound-anchor critical error: %w", err)
	}

	return nil
}

// isTemporaryAnchorError 判断是否是临时性错误（可以使用 fallback）
func (sm *SystemManager) isTemporaryAnchorError(err error, output string) bool {
	// 这些错误被认为是临时性的，可以使用嵌入的 root.key
	temporaryErrors := []string{
		"timeout",             // 超时
		"network unreachable", // 网络不可达
		"connection refused",  // 连接拒绝
		"resolution failed",   // DNS 解析失败
		"no address",          // 无法解析地址
		"could not fetch",     // 无法获取
		"no such file",        // 文件不存在（unbound-anchor 可能不存在）
		"command not found",   // 命令不存在
	}

	outputLower := strings.ToLower(output) + strings.ToLower(err.Error())

	for _, errPattern := range temporaryErrors {
		if strings.Contains(outputLower, errPattern) {
			return true
		}
	}

	return false
}

// extractEmbeddedRootKey 从嵌入文件中提取 root.key
func (sm *SystemManager) extractEmbeddedRootKey(targetPath string) error {
	// 从嵌入的文件系统中读取 root.key
	data, err := unboundBinaries.ReadFile("data/root.key")
	if err != nil {
		return fmt.Errorf("embedded root.key not found: %w", err)
	}

	// 写入目标路径
	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write embedded root.key to %s: %w", targetPath, err)
	}

	return nil
}
