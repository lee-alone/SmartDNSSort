package recursor

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"smartdnssort/logger"
	"strings"
	"time"
)

const (
	// RootZoneURL 官方root.zone下载源
	RootZoneURL = "https://www.internic.net/domain/root.zone"

	// RootZoneFilename root.zone文件名
	RootZoneFilename = "root.zone"

	// RootZoneUpdateInterval 更新间隔（7天）
	RootZoneUpdateInterval = 7 * 24 * time.Hour

	// 下载配置
	DownloadTimeout = 30 * time.Second // 下载超时
	ValidateTimeout = 5 * time.Second  // 验证超时（未使用，保留供扩展）
	MaxRetries      = 3                // 最大重试次数
	RetryDelay      = 5 * time.Second  // 重试延迟
	MinFileSize     = 100000           // 最小文件大小 100KB
	MaxFileSize     = 10 * 1024 * 1024 // 最大文件大小 10MB
)

// RootZoneManager 管理root.zone文件
type RootZoneManager struct {
	dataDir      string
	rootZonePath string
	client       *http.Client
}

// NewRootZoneManager 创建RootZoneManager
func NewRootZoneManager() *RootZoneManager {
	// 根据平台选择存储位置
	var configDir string

	if runtime.GOOS == "linux" {
		// Linux 上使用系统目录
		configDir = "/etc/unbound"
	} else {
		// Windows 和其他平台使用程序目录
		var err error
		configDir, err = GetUnboundConfigDir()
		if err != nil {
			// 如果获取失败，使用备用路径
			configDir = "unbound"
			_ = os.MkdirAll(configDir, 0755)
		}
	}

	// 确保目录存在
	_ = os.MkdirAll(configDir, 0755)

	return &RootZoneManager{
		dataDir:      configDir,
		rootZonePath: filepath.Join(configDir, RootZoneFilename),
		// 配置HTTP客户端，设置下载超时
		client: &http.Client{
			Timeout: DownloadTimeout,
		},
	}
}

// EnsureRootZone 确保root.zone文件存在
// 如果文件不存在，则下载；如果存在且未过期，则保持不变
// 返回文件路径和是否是新建文件
func (rm *RootZoneManager) EnsureRootZone() (string, bool, error) {
	return rm.ensureRootZoneWithRetry(MaxRetries)
}

// ensureRootZoneWithRetry 确保root.zone文件存在（带重试）
func (rm *RootZoneManager) ensureRootZoneWithRetry(maxRetries int) (string, bool, error) {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			logger.Infof("[RootZone] Retry attempt %d/%d after %v", attempt, maxRetries, RetryDelay)
			time.Sleep(RetryDelay)
		}

		// 检查文件是否存在
		exists, err := rm.fileExists()
		if err != nil {
			lastErr = fmt.Errorf("failed to check root.zone existence: %w", err)
			continue
		}

		if !exists {
			// 文件不存在，优先尝试从嵌入数据解压
			logger.Infof("[RootZone] root.zone not found, attempting to extract from embedded data")
			if err := rm.extractEmbeddedRootZone(); err == nil {
				logger.Infof("[RootZone] root.zone extracted successfully from embedded data")
				return rm.rootZonePath, true, nil
			}

			// 如果解压失败，尝试从网络下载
			logger.Infof("[RootZone] Failed to extract from embedded data, downloading from %s", RootZoneURL)
			if err := rm.downloadRootZone(); err != nil {
				lastErr = fmt.Errorf("failed to download root.zone: %w", err)
				// 如果是临时错误，继续重试
				if rm.isTemporaryDownloadError(err) {
					logger.Warnf("[RootZone] Temporary download error on attempt %d: %v", attempt, err)
					continue
				}
				// 永久错误，不重试
				logger.Errorf("[RootZone] Permanent download error: %v", err)
				return "", false, lastErr
			}
			logger.Infof("[RootZone] root.zone downloaded successfully")
			return rm.rootZonePath, true, nil
		}

		// 文件存在，检查是否需要更新
		shouldUpdate, err := rm.shouldUpdate()
		if err != nil {
			logger.Warnf("[RootZone] Failed to check if root.zone needs update: %v", err)
			// 即使检查失败，也使用现有文件
			return rm.rootZonePath, false, nil
		}

		if !shouldUpdate {
			logger.Debugf("[RootZone] root.zone exists and is up to date")
			return rm.rootZonePath, false, nil
		}

		// 文件需要更新，下载新版本
		logger.Infof("[RootZone] root.zone is outdated, updating...")
		if err := rm.downloadRootZone(); err != nil {
			lastErr = fmt.Errorf("failed to update root.zone: %w", err)
			// 如果是临时错误，继续重试
			if rm.isTemporaryDownloadError(err) {
				logger.Warnf("[RootZone] Temporary update error on attempt %d: %v", attempt, err)
				continue
			}
			// 永久错误或非更新场景，使用现有文件
			logger.Warnf("[RootZone] Failed to update root.zone (attempt %d), using existing file: %v", attempt, err)
			return rm.rootZonePath, false, nil
		}
		logger.Infof("[RootZone] root.zone updated successfully")
		return rm.rootZonePath, true, nil
	}

	// 所有重试都失败
	return "", false, fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

// fileExists 检查root.zone文件是否存在且有效
func (rm *RootZoneManager) fileExists() (bool, error) {
	info, err := os.Stat(rm.rootZonePath)
	if err == nil {
		// 检查文件大小（root.zone通常2-3MB，最小应该100KB）
		if info.Size() < 100000 {
			logger.Warnf("[RootZone] root.zone file too small (%d bytes), will re-download", info.Size())
			return false, nil // 视为不存在，触发重新下载
		}
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// extractEmbeddedRootZone 从嵌入的数据中解压 root.zone 文件
// 这是初始化时的首选方式，避免网络依赖
func (rm *RootZoneManager) extractEmbeddedRootZone() error {
	// 确保目录存在
	if err := os.MkdirAll(rm.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", rm.dataDir, err)
	}

	// 读取嵌入的 root.zone 文件
	data, err := unboundBinaries.ReadFile("data/root.zone")
	if err != nil {
		return fmt.Errorf("root.zone not found in embedded data: %w", err)
	}

	// 验证嵌入数据的大小
	if len(data) < MinFileSize {
		return fmt.Errorf("embedded root.zone too small: %d bytes (expected > %d bytes)", len(data), MinFileSize)
	}

	// 写入到目标位置
	if err := os.WriteFile(rm.rootZonePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write root.zone to %s: %w", rm.rootZonePath, err)
	}

	// 验证写入的文件
	info, err := os.Stat(rm.rootZonePath)
	if err != nil {
		return fmt.Errorf("failed to verify root.zone after extraction: %w", err)
	}

	if info.Size() != int64(len(data)) {
		return fmt.Errorf("root.zone size mismatch after extraction: expected %d, got %d", len(data), info.Size())
	}

	logger.Infof("[RootZone] root.zone extracted from embedded data: %s (%d bytes)", rm.rootZonePath, info.Size())
	return nil
}

// shouldUpdate 检查是否需要更新root.zone
func (rm *RootZoneManager) shouldUpdate() (bool, error) {
	info, err := os.Stat(rm.rootZonePath)
	if err != nil {
		return false, err
	}

	// 检查文件修改时间
	timeSinceUpdate := time.Since(info.ModTime())
	return timeSinceUpdate > RootZoneUpdateInterval, nil
}

// downloadRootZone 下载root.zone文件
func (rm *RootZoneManager) downloadRootZone() error {
	// 下载到临时文件
	tempPath := rm.rootZonePath + ".tmp"

	resp, err := rm.client.Get(RootZoneURL)
	if err != nil {
		return fmt.Errorf("failed to download root.zone: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download root.zone: HTTP %d", resp.StatusCode)
	}

	// 检查Content-Length（如果服务器提供）
	expectedSize := resp.ContentLength
	if expectedSize > 0 {
		if expectedSize < MinFileSize {
			return fmt.Errorf("root.zone size too small (from headers): %d bytes (expected > %d bytes)", expectedSize, MinFileSize)
		}
		if expectedSize > MaxFileSize {
			logger.Warnf("[RootZone] root.zone size seems too large (from headers): %d bytes", expectedSize)
		}
	}

	// 创建临时文件
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	written, err := io.Copy(tempFile, resp.Body)
	tempFile.Close()

	if err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to write root.zone: %w", err)
	}

	// 验证写入大小与预期大小是否匹配
	if expectedSize > 0 && written != expectedSize {
		_ = os.Remove(tempPath)
		return fmt.Errorf("root.zone download incomplete: got %d bytes, expected %d bytes", written, expectedSize)
	}

	// 验证文件内容
	if err := rm.validateRootZone(tempPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("root.zone validation failed: %w", err)
	}

	// 原子替换旧文件
	if err := os.Rename(tempPath, rm.rootZonePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to replace root.zone: %w", err)
	}

	// 确保文件权限正确
	if err := os.Chmod(rm.rootZonePath, 0644); err != nil {
		logger.Warnf("[RootZone] Failed to set permissions on root.zone: %v", err)
	}

	return nil
}

// isTemporaryDownloadError 判断是否是临时下载错误
func (rm *RootZoneManager) isTemporaryDownloadError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	temporaryErrors := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"network unreachable",
		"no such host",
		"temporary failure",
		"i/o timeout",
		"connection timed out",
	}

	for _, pattern := range temporaryErrors {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}
	return false
}

// validateRootZone 验证root.zone文件的基本有效性
func (rm *RootZoneManager) validateRootZone(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := string(data)

	// 1. 检查文件大小（root.zone通常2-3MB，最小应该100KB）
	if len(data) < 100000 {
		return fmt.Errorf("root.zone file too small: %d bytes (expected > 100KB)", len(data))
	}

	// 2. 检查是否包含zone文件标记（至少包含一个）
	if !strings.Contains(content, "$ORIGIN") && !strings.Contains(content, "$TTL") {
		return fmt.Errorf("invalid root.zone format: missing zone file markers ($ORIGIN or $TTL)")
	}

	// 3. 检查是否包含SOA记录（root.zone必须有）
	if !strings.Contains(content, "SOA") {
		return fmt.Errorf("invalid root.zone format: missing SOA record")
	}

	// 4. 检查是否包含NS记录（根域必须有）
	if !strings.Contains(content, "NS") {
		return fmt.Errorf("invalid root.zone format: missing NS records")
	}

	return nil
}

// GetRootZoneConfig 获取auth-zone配置字符串
// 返回适合Unbound配置文件使用的auth-zone配置
//
// 配置策略：启用自动更新
// - 配置多个根服务器作为 primary，让 unbound 自动从它们同步 root.zone
// - 这样 unbound 会定期检查根服务器的更新，无需我们手动更新
// - 如果网络断开，fallback-enabled 会回退到普通递归查询
// - for-downstream: no 保护隐私，不向外部暴露根区数据
func (rm *RootZoneManager) GetRootZoneConfig() (string, error) {
	// 获取文件路径
	zonePath := rm.rootZonePath

	// 在Windows上，路径需要转换为正斜杠（unbound配置要求）
	if runtime.GOOS == "windows" {
		zonePath = strings.ReplaceAll(zonePath, "\\", "/")
	}

	// 生成auth-zone配置，启用自动更新
	// 根服务器列表（IPv4 和 IPv6）
	// 这些是官方根服务器，unbound 会从它们同步 root.zone
	config := fmt.Sprintf(`
    # 使用本地root.zone文件，并启用自动更新
    # Unbound 会定期从根服务器同步最新的根区数据
    # 无需我们手动更新，完全自动化
    auth-zone:
        name: "."
        zonefile: "%s"
        
        # 配置根服务器作为权威数据源，启用自动同步
        # 使用多个根服务器提高可靠性
        primary: 192.0.32.132      # b.root-servers.net (IPv4)
        primary: 192.0.47.132      # x.root-servers.net (IPv4)
        primary: 2001:500:12::d0d  # b.root-servers.net (IPv6)
        primary: 2001:500:1::53    # x.root-servers.net (IPv6)
        
        # 极其重要：如果网络彻底断了同步不了，回退到普通递归查询
        fallback-enabled: yes
        
        # 让递归模块使用这里的本地根数据，加速递归查询
        for-upstream: yes
        
        # 不向外部用户暴露根区数据，保护隐私
        for-downstream: no
`, zonePath)

	return config, nil
}

// UpdateRootZonePeriodically 定期更新root.zone（已弃用）
//
// 注意：此方法已被弃用，不再使用
// 现在 unbound 通过 auth-zone 配置自动从根服务器同步 root.zone
// 这样更高效，无需我们手动定期更新
//
// 保留此方法以保持向后兼容性，但不再被调用
// 这个函数应该在一个单独的goroutine中调用
func (rm *RootZoneManager) UpdateRootZonePeriodically(stopCh <-chan struct{}) {
	ticker := time.NewTicker(RootZoneUpdateInterval)
	defer ticker.Stop()

	logger.Infof("[RootZone] Started periodic root.zone update (interval: %v)", RootZoneUpdateInterval)

	var lastUpdateTime time.Time
	var consecutiveFailures int
	const maxConsecutiveFailures = 3

	for {
		select {
		case <-stopCh:
			logger.Infof("[RootZone] Stopping periodic update")
			return
		case <-ticker.C:
			logger.Debugf("[RootZone] Checking for root.zone update...")
			_, updated, err := rm.EnsureRootZone()

			if err != nil {
				consecutiveFailures++
				logger.Errorf("[RootZone] Failed to update root.zone (attempt %d/%d): %v",
					consecutiveFailures, maxConsecutiveFailures, err)

				if consecutiveFailures >= maxConsecutiveFailures {
					logger.Warnf("[RootZone] Max consecutive failures (%d) reached, will retry next cycle", maxConsecutiveFailures)
					consecutiveFailures = 0
				}
				continue
			}

			// 更新成功
			consecutiveFailures = 0

			if updated {
				lastUpdateTime = time.Now()
				logger.Infof("[RootZone] root.zone updated successfully at %s", lastUpdateTime.Format(time.RFC3339))
			} else {
				logger.Debugf("[RootZone] root.zone is already up to date")
			}
		}
	}
}
