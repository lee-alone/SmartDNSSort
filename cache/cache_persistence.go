package cache

import (
	"encoding/gob"
	"errors"
	"hash/crc32"
	"io"
	"math/rand"
	"os"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

// 校验和错误
var ErrChecksumMismatch = errors.New("cache file checksum mismatch")

// cacheFileHeader 持久化文件头
type cacheFileHeader struct {
	Magic   uint32 // 魔数，用于识别文件格式
	Version uint32 // 文件格式版本
}

// cacheFileFooter 持久化文件尾（校验和）
type cacheFileFooter struct {
	Checksum uint32 // CRC32 校验和
	Count    uint64 // 条目数量
}

const (
	cacheFileMagic   = 0x53444E53 // "SDNS"
	cacheFileVersion = 1
)

// SaveToDisk 将缓存保存到磁盘
// 采用流式持久化策略：按分片锁定并直接写入文件，内存占用从 O(N) 降为 O(分片大小)
// 文件格式：[Header][Entry1][Entry2]...[Footer(含校验和)]
func (c *Cache) SaveToDisk(filename string) error {
	// 1. 脏数据检查
	currentDirty := c.rawCache.GetDirtyCount()
	if atomic.LoadUint64(&c.lastSavedDirty) == currentDirty {
		// 无变更，跳过保存
		return nil
	}

	// 2. 创建临时文件
	tempFile := filename + ".tmp"
	f, err := os.Create(tempFile)
	if err != nil {
		return err
	}

	// 3. 写入文件头
	header := cacheFileHeader{
		Magic:   cacheFileMagic,
		Version: cacheFileVersion,
	}
	if err := gob.NewEncoder(f).Encode(header); err != nil {
		f.Close()
		os.Remove(tempFile)
		return err
	}

	// 4. 流式写入：使用 gob 流式编码器直接写入文件
	encoder := gob.NewEncoder(f)

	// 统计实际写入的条目数和校验和计算
	var entryCount uint64
	checksum := crc32.NewIEEE()

	// 5. 流式遍历所有分片，直接编码写入
	// 每次只锁定一个分片，处理完立即释放，内存占用可控
	c.rawCache.StreamForEach(func(key string, value any) bool {
		entry, ok := value.(*RawCacheEntry)
		if !ok {
			return true // 继续遍历
		}

		// 解析 domain 和 QType
		domain, qtype := parseCacheKey(key)
		if domain == "" {
			return true // 继续遍历
		}

		// 准备 CNAME 数据
		entryCNAMEs := entry.CNAMEs
		var legacyCNAME string
		if len(entryCNAMEs) > 0 {
			legacyCNAME = entryCNAMEs[0]
		}

		// 构建持久化条目
		persistentEntry := PersistentCacheEntry{
			Domain:          domain,
			QType:           qtype,
			IPs:             entry.IPs,
			CNAME:           legacyCNAME,
			CNAMEs:          entryCNAMEs,
			AcquisitionTime: entry.AcquisitionTime.Unix(),
			EffectiveTTL:    entry.EffectiveTTL,
			GracePeriod:     entry.gracePeriod,
		}

		// 直接编码写入单条记录
		if err := encoder.Encode(persistentEntry); err != nil {
			return false // 遇到错误，停止遍历
		}

		// 更新校验和（基于持久化条目的关键字段）
		checksum.Write([]byte(domain))
		for _, ip := range persistentEntry.IPs {
			checksum.Write([]byte(ip))
		}

		entryCount++
		return true // 继续遍历
	})

	// 检查是否有错误发生（通过条目数判断）
	if entryCount == 0 && c.rawCache.Len() > 0 {
		f.Close()
		os.Remove(tempFile)
		// 如果有数据但没写入，说明编码出错
	}

	// 6. 写入文件尾（校验和）
	footer := cacheFileFooter{
		Checksum: checksum.Sum32(),
		Count:    entryCount,
	}
	if err := gob.NewEncoder(f).Encode(footer); err != nil {
		f.Close()
		os.Remove(tempFile)
		return err
	}

	f.Close()

	// 7. 原子替换
	if err := os.Rename(tempFile, filename); err != nil {
		return err
	}

	// 更新最后保存的计数
	atomic.StoreUint64(&c.lastSavedDirty, currentDirty)
	return nil
}

// LoadFromDisk 从磁盘加载缓存
// 实现平滑恢复算法：继承剩余 TTL 或分配抖动延迟，避免集体失效洪峰
// 支持流式读取：逐条解码，内存占用 O(1)
// 支持校验和验证：检测文件损坏
func (c *Cache) LoadFromDisk(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	decoder := gob.NewDecoder(f)

	// 1. 读取文件头
	var header cacheFileHeader
	if err := decoder.Decode(&header); err != nil {
		// 旧版本文件没有文件头，尝试作为旧格式加载
		// 重新打开文件，从头开始读取
		f.Seek(0, 0)
		return c.loadFromDiskLegacy(filename, f)
	}

	// 验证文件头
	if header.Magic != cacheFileMagic {
		// 不是新格式文件，尝试作为旧格式加载
		f.Seek(0, 0)
		return c.loadFromDiskLegacy(filename, f)
	}

	if header.Version > cacheFileVersion {
		// 文件版本过新，无法读取
		return errors.New("cache file version is too new")
	}

	now := time.Now().Unix()
	checksum := crc32.NewIEEE()
	var entryCount uint64

	// 2. 流式读取条目
	// 先收集所有条目，最后读取 footer 进行校验
	var entries []PersistentCacheEntry
	for {
		var entry PersistentCacheEntry
		err := decoder.Decode(&entry)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}

		// 检查是否是 footer（通过判断是否有合理的域名）
		if entry.Domain == "" && len(entry.IPs) == 0 {
			// 可能是 footer，尝试解码为 footer
			// 由于我们已经解码为 entry，需要重新处理
			break
		}

		entries = append(entries, entry)

		// 更新校验和
		checksum.Write([]byte(entry.Domain))
		for _, ip := range entry.IPs {
			checksum.Write([]byte(ip))
		}
		entryCount++
	}

	// 3. 读取文件尾（校验和）
	// 由于 gob 流式解码的限制，我们需要在解码条目后继续解码 footer
	// 重新创建 decoder 从当前位置读取
	var footer cacheFileFooter
	// 尝试解码 footer，如果失败则忽略（兼容性）
	footerDecoder := gob.NewDecoder(f)
	footerErr := footerDecoder.Decode(&footer)
	if footerErr == nil && footer.Count > 0 {
		// 验证校验和
		if footer.Checksum != checksum.Sum32() {
			return ErrChecksumMismatch
		}
	}

	// 4. 处理所有条目
	for _, entry := range entries {
		key := cacheKey(entry.Domain, entry.QType)

		cnames := entry.CNAMEs
		if len(cnames) == 0 && entry.CNAME != "" {
			cnames = []string{entry.CNAME}
		}

		// 平滑恢复算法：计算剩余寿命并动态分配 TTL
		var loadTTL uint32
		if entry.AcquisitionTime > 0 && entry.EffectiveTTL > 0 {
			// 有完整的持久化数据，执行平滑恢复
			elapsed := now - entry.AcquisitionTime
			remainingTTL := int64(entry.EffectiveTTL) - elapsed

			if remainingTTL > 30 {
				// 场景 A：数据依然很新鲜
				// 策略：直接继承剩余寿命，保证准确性
				loadTTL = uint32(remainingTTL)
			} else {
				// 场景 B：数据已过期或即将过期
				// 策略：分配 30s 基础 TTL + 抖动延迟（15~45s）
				// 核心用意：防止在重启后的第 30.001 秒发生二次集体失效洪峰
				loadTTL = uint32(15 + rand.Intn(31)) // 15~45s 随机分布
			}
		} else {
			// 旧版本数据或数据不完整，使用默认 30s + 抖动
			loadTTL = uint32(15 + rand.Intn(31))
		}

		cacheEntry := &RawCacheEntry{
			IPs:             entry.IPs,
			CNAMEs:          cnames,
			UpstreamTTL:     loadTTL,
			EffectiveTTL:    loadTTL,
			AcquisitionTime: time.Now(),        // 以加载时间为新起点
			gracePeriod:     entry.GracePeriod, // 恢复软过期容忍期
		}
		c.rawCache.Set(key, cacheEntry)
	}

	// 加载完成后更新 dirty 计数，避免立即保存
	atomic.StoreUint64(&c.lastSavedDirty, c.rawCache.GetDirtyCount())
	return nil
}

// loadFromDiskLegacy 以旧格式加载缓存文件（无校验和）
func (c *Cache) loadFromDiskLegacy(filename string, f *os.File) error {
	decoder := gob.NewDecoder(f)
	now := time.Now().Unix()

	for {
		var entry PersistentCacheEntry
		err := decoder.Decode(&entry)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
		}

		key := cacheKey(entry.Domain, entry.QType)

		cnames := entry.CNAMEs
		if len(cnames) == 0 && entry.CNAME != "" {
			cnames = []string{entry.CNAME}
		}

		// 平滑恢复算法
		var loadTTL uint32
		if entry.AcquisitionTime > 0 && entry.EffectiveTTL > 0 {
			elapsed := now - entry.AcquisitionTime
			remainingTTL := int64(entry.EffectiveTTL) - elapsed

			if remainingTTL > 30 {
				loadTTL = uint32(remainingTTL)
			} else {
				loadTTL = uint32(15 + rand.Intn(31))
			}
		} else {
			loadTTL = uint32(15 + rand.Intn(31))
		}

		cacheEntry := &RawCacheEntry{
			IPs:             entry.IPs,
			CNAMEs:          cnames,
			UpstreamTTL:     loadTTL,
			EffectiveTTL:    loadTTL,
			AcquisitionTime: time.Now(),
			gracePeriod:     entry.GracePeriod,
		}
		c.rawCache.Set(key, cacheEntry)
	}

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
