package cache

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// SaveToDisk 将缓存保存到磁盘
// 采用原子写入策略：先写入临时文件，再重命名，防止写入中断导致文件损坏
func (c *Cache) SaveToDisk(filename string) error {
	c.mu.RLock()
	// 从 LRUCache 获取所有原始缓存项的快照
	cacheSnapshot := c.getRawCacheSnapshot()
	allKeys := c.getRawCacheKeysSnapshot()
	c.mu.RUnlock()

	// 构建条目列表
	var entries []PersistentCacheEntry
	for i, key := range allKeys {
		if i >= len(cacheSnapshot) {
			break
		}
		entry := cacheSnapshot[i]

		domain := c.extractDomain(key)
		// Extract QType from key (format: domain#qtype_char)
		parts := strings.Split(key, "#")
		if len(parts) != 2 {
			continue
		}
		// Convert string back to rune then to uint16
		qtype := uint16([]rune(parts[1])[0])

		// 优先写入 CNAMEs
		entryCNAMEs := entry.CNAMEs
		var legacyCNAME string
		if len(entryCNAMEs) > 0 {
			legacyCNAME = entryCNAMEs[0]
		}

		entries = append(entries, PersistentCacheEntry{
			Domain: domain,
			QType:  qtype,
			IPs:    entry.IPs,
			CNAME:  legacyCNAME, // 写入旧字段以保持兼容性
			CNAMEs: entryCNAMEs,
		})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}

	// 写入临时文件
	tempFile := filename + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return err
	}

	// 原子替换（在 Windows 上 Go 的 os.Rename 会尝试覆盖目标文件）
	return os.Rename(tempFile, filename)
}

// LoadFromDisk 从磁盘加载缓存
func (c *Cache) LoadFromDisk(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, nothing to load
		}
		return err
	}

	var entries []PersistentCacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range entries {
		key := cacheKey(entry.Domain, entry.QType)

		// 迁移逻辑：如果 CNAMEs 为空但 CNAME 不为空，则转换
		cnames := entry.CNAMEs
		if len(cnames) == 0 && entry.CNAME != "" {
			cnames = []string{entry.CNAME}
		}

		cacheEntry := &RawCacheEntry{
			IPs:             entry.IPs,
			CNAMEs:          cnames,
			UpstreamTTL:     300, // Default 5 minutes as we don't persist TTL
			AcquisitionTime: time.Now(),
		}
		c.rawCache.Set(key, cacheEntry)
	}
	return nil
}

// GetMsg 获取 DNSSEC 完整消息缓存
func (c *Cache) GetMsg(domain string, qtype uint16) (*dns.Msg, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := cacheKey(domain, qtype)
	val, exists := c.msgCache.Get(key)
	if !exists {
		return nil, false
	}

	entry := val.(*DNSSECCacheEntry)
	if entry.IsExpired() {
		// 缓存已过期，删除它
		c.msgCache.Delete(key)
		return nil, false
	}

	// 返回消息副本以防止外部修改原始缓存
	msgCopy := entry.Message.Copy()
	return msgCopy, true
}

// SetMsg 设置 DNSSEC 完整消息缓存
// 自动从消息中提取最小 TTL 作为缓存生命周期
func (c *Cache) SetMsg(domain string, qtype uint16, msg *dns.Msg) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 计算最小 TTL
	minTTL := extractMinTTLFromMsg(msg)
	if minTTL == 0 {
		minTTL = 300 // 默认 5 分钟
	}

	key := cacheKey(domain, qtype)
	entry := &DNSSECCacheEntry{
		Message:         msg.Copy(), // 保存副本
		AcquisitionTime: time.Now(),
		TTL:             minTTL,
	}
	c.msgCache.Set(key, entry)
}

// extractMinTTLFromMsg 从 DNS 消息中提取最小 TTL
func extractMinTTLFromMsg(msg *dns.Msg) uint32 {
	minTTL := uint32(0)

	// 检查 Answer 部分
	for _, rr := range msg.Answer {
		ttl := rr.Header().Ttl
		if minTTL == 0 || ttl < minTTL {
			minTTL = ttl
		}
	}

	// 检查 Authority 部分（用于 RRSIG）
	for _, rr := range msg.Ns {
		ttl := rr.Header().Ttl
		if minTTL == 0 || ttl < minTTL {
			minTTL = ttl
		}
	}

	return minTTL
}
