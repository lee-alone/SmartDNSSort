package ping

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// IPFailureRecord 记录单个IP的失效信息
type IPFailureRecord struct {
	IP              string    `json:"ip"`
	FailureCount    int       `json:"failure_count"`     // 累计失效次数
	LastFailureTime time.Time `json:"last_failure_time"` // 最后失效时间
	SuccessCount    int       `json:"success_count"`     // 连续成功次数
	TotalAttempts   int       `json:"total_attempts"`    // 总尝试次数
	FailureRate     float64   `json:"failure_rate"`      // 失效率
}

// IPFailureWeightManager IP失效权重管理器
// 用于记录域名解析出来的IP在实际使用中的失效情况
type IPFailureWeightManager struct {
	mu              sync.RWMutex
	records         map[string]*IPFailureRecord
	persistFile     string
	decayDays       int // 权重衰减周期（天）
	maxFailureCount int // 最大失效计数（防止溢出）
}

// NewIPFailureWeightManager 创建IP失效权重管理器
func NewIPFailureWeightManager(persistFile string) *IPFailureWeightManager {
	m := &IPFailureWeightManager{
		records:         make(map[string]*IPFailureRecord),
		persistFile:     persistFile,
		decayDays:       7,   // 7天衰减周期
		maxFailureCount: 100, // 最多记录100次失效
	}
	m.loadFromDisk()
	return m
}

// RecordFailure 记录IP失效
func (m *IPFailureWeightManager) RecordFailure(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, exists := m.records[ip]
	if !exists {
		record = &IPFailureRecord{IP: ip}
		m.records[ip] = record
	}

	record.FailureCount++
	if record.FailureCount > m.maxFailureCount {
		record.FailureCount = m.maxFailureCount
	}
	record.LastFailureTime = time.Now()
	record.SuccessCount = 0 // 重置成功计数
	record.TotalAttempts++
	m.updateFailureRate(record)
}

// RecordSuccess 记录IP成功
func (m *IPFailureWeightManager) RecordSuccess(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, exists := m.records[ip]
	if !exists {
		record = &IPFailureRecord{IP: ip}
		m.records[ip] = record
	}

	record.SuccessCount++
	record.TotalAttempts++
	m.updateFailureRate(record)

	// 连续成功3次后，降低失效计数
	if record.SuccessCount >= 3 && record.FailureCount > 0 {
		record.FailureCount--
		record.SuccessCount = 0
	}
}

// updateFailureRate 更新失效率
func (m *IPFailureWeightManager) updateFailureRate(record *IPFailureRecord) {
	if record.TotalAttempts > 0 {
		record.FailureRate = float64(record.FailureCount) / float64(record.TotalAttempts)
	}
}

// GetWeight 获取IP的权重值（用于排序调整）
// 返回值越大，IP排序越靠后
func (m *IPFailureWeightManager) GetWeight(ip string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	record, exists := m.records[ip]
	if !exists {
		return 0
	}

	// 基础权重：每次失效增加50ms
	weight := record.FailureCount * 50

	// 时间衰减：距离最后失效越久，权重越低
	if !record.LastFailureTime.IsZero() {
		daysSinceFailure := time.Since(record.LastFailureTime).Hours() / 24
		if daysSinceFailure > float64(m.decayDays) {
			// 超过衰减周期，权重清零
			weight = 0
		} else {
			// 线性衰减
			decayFactor := daysSinceFailure / float64(m.decayDays)
			weight = int(float64(weight) * (1 - decayFactor))
		}
	}

	return weight
}

// GetRecord 获取IP的失效记录
func (m *IPFailureWeightManager) GetRecord(ip string) *IPFailureRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	record, exists := m.records[ip]
	if !exists {
		return &IPFailureRecord{IP: ip}
	}

	// 返回副本
	copy := *record
	return &copy
}

// SaveToDisk 保存失效记录到磁盘
func (m *IPFailureWeightManager) SaveToDisk() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.persistFile == "" {
		return nil
	}

	records := make([]*IPFailureRecord, 0, len(m.records))
	for _, r := range m.records {
		records = append(records, r)
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}

	tempFile := m.persistFile + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return err
	}

	return os.Rename(tempFile, m.persistFile)
}

// loadFromDisk 从磁盘加载失效记录
func (m *IPFailureWeightManager) loadFromDisk() error {
	if m.persistFile == "" {
		return nil
	}

	data, err := os.ReadFile(m.persistFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var records []*IPFailureRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, r := range records {
		m.records[r.IP] = r
	}
	return nil
}

// GetAllRecords 获取所有IP的失效记录
func (m *IPFailureWeightManager) GetAllRecords() []*IPFailureRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	records := make([]*IPFailureRecord, 0, len(m.records))
	for _, r := range m.records {
		copy := *r
		records = append(records, &copy)
	}
	return records
}

// Clear 清空所有记录
func (m *IPFailureWeightManager) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = make(map[string]*IPFailureRecord)
}
