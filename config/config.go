package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

const (
	// AvgBytesPerDomain 估算每个域名在缓存中占用的平均字节数（包含A和AAAA记录及辅助结构）
	// 基于以下假设：RawCacheEntry、SortedCacheEntry、map开销、域名/IP字符串存储
	// 估算细节 (字节):
	// - RawCacheEntry (含IP): ~80
	// - SortedCacheEntry (含IP): ~40
	// - 域名字符串 (e.g., "www.example.com"): ~20
	// - IPs (4个IPv4, 4个IPv6): (4 * 16) + (4 * 46) = 64 + 184 = 248
	// - Map 开销 (rawCache, sortedCache 等): ~100
	// - 预取列表条目: ~50
	// - 其他运行时开销: 282
	// 总计: 80+40+20+248+100+50+282 = 820
	AvgBytesPerDomain = 820
)

// CalculateMaxEntries 根据最大内存限制计算最大缓存条目数
func (c *CacheConfig) CalculateMaxEntries() int {
	if c.MaxMemoryMB <= 0 {
		// 如果未设置最大内存限制，返回 0 表示不限制
		return 0
	}
	// 最大内存 (字节) / 每个域名的平均字节数
	return (c.MaxMemoryMB * 1024 * 1024) / AvgBytesPerDomain
}

// CreateDefaultConfig 创建默认配置文件
func CreateDefaultConfig(filePath string) error {
	return os.WriteFile(filePath, []byte(DefaultConfigContent), 0644)
}

// ValidateAndRepairConfig 验证并修复配置文件中缺失的字段
// 该函数只在配置文件不存在时创建默认配置
// 如果配置文件已存在，不做任何修改，以保留用户的注释和自定义配置
func ValidateAndRepairConfig(filePath string) error {
	// 如果文件不存在, 直接创建默认配置
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return CreateDefaultConfig(filePath)
	}

	// 配置文件已存在，不做任何修改
	// 这样可以保留用户的注释和零值配置（如 max_cpu_cores: 0）
	// LoadConfig 函数会负责设置缺失字段的默认值
	return nil
}

// LoadConfig 从YAML文件加载配置
func LoadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		// 如果文件不存在，自动创建默认配置文件
		if os.IsNotExist(err) {
			if err := CreateDefaultConfig(filePath); err != nil {
				return nil, err
			}
			// 读取刚创建的文件
			data, err = os.ReadFile(filePath)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// 设置默认值
	setDefaultValues(&cfg, data)

	return &cfg, nil
}
