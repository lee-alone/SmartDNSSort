package cache

import (
	"encoding/gob"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

// SaveToDisk 将缓存保存到磁盘
// 采用原子写入策略：直接使用 Gob 流式编码写入临时文件，再重命名
func (c *Cache) SaveToDisk(filename string) error {
	// 1. 脏数据检查
	currentDirty := c.rawCache.GetDirtyCount()
	if atomic.LoadUint64(&c.lastSavedDirty) == currentDirty {
		// 无变更，跳过保存
		return nil
	}

	// 2. 获取一致性快照 (分片锁定保证安全)
	snapshot := c.rawCache.GetSnapshot()

	// 3. 准备持久化条目
	entries := make([]PersistentCacheEntry, 0, len(snapshot))
	for _, s := range snapshot {
		entry, ok := s.Value.(*RawCacheEntry)
		if !ok {
			continue
		}

		domain := c.extractDomain(s.Key)
		// Extract QType from key (format: domain#qtype_char)
		parts := strings.Split(s.Key, "#")
		if len(parts) != 2 {
			continue
		}
		qtype := uint16([]rune(parts[1])[0])

		entryCNAMEs := entry.CNAMEs
		var legacyCNAME string
		if len(entryCNAMEs) > 0 {
			legacyCNAME = entryCNAMEs[0]
		}

		entries = append(entries, PersistentCacheEntry{
			Domain: domain,
			QType:  qtype,
			IPs:    entry.IPs,
			CNAME:  legacyCNAME,
			CNAMEs: entryCNAMEs,
		})
	}

	// 4. 原子写入
	tempFile := filename + ".tmp"
	f, err := os.Create(tempFile)
	if err != nil {
		return err
	}

	// 使用 Encoder 直接流式写入，减少大块内存分配
	encoder := gob.NewEncoder(f)
	if err := encoder.Encode(entries); err != nil {
		f.Close()
		os.Remove(tempFile)
		return err
	}
	f.Close()

	// 原子替换
	if err := os.Rename(tempFile, filename); err != nil {
		return err
	}

	// 更新最后保存的计数
	atomic.StoreUint64(&c.lastSavedDirty, currentDirty)
	return nil
}

// LoadFromDisk 从磁盘加载缓存
func (c *Cache) LoadFromDisk(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	var entries []PersistentCacheEntry
	decoder := gob.NewDecoder(f)
	if err := decoder.Decode(&entries); err != nil {
		return err
	}

	for _, entry := range entries {
		key := cacheKey(entry.Domain, entry.QType)

		cnames := entry.CNAMEs
		if len(cnames) == 0 && entry.CNAME != "" {
			cnames = []string{entry.CNAME}
		}

		cacheEntry := &RawCacheEntry{
			IPs:             entry.IPs,
			CNAMEs:          cnames,
			UpstreamTTL:     300,
			AcquisitionTime: time.Now(),
		}
		c.rawCache.Set(key, cacheEntry)
	}

	// 加载完成后更新 dirty 计数，避免立即保存
	atomic.StoreUint64(&c.lastSavedDirty, c.rawCache.GetDirtyCount())
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
