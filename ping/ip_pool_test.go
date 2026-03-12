package ping

import (
	"sync"
	"testing"
	"time"
)

func TestNewIPPool(t *testing.T) {
	pool := NewIPPool()
	if pool == nil {
		t.Fatal("NewIPPool returned nil")
	}
	if pool.ips == nil {
		t.Fatal("IPPool.ips map is not initialized")
	}
}

func TestUpdateDomainIPs(t *testing.T) {
	pool := NewIPPool()

	// 测试添加新 IP
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1", "2.2.2.2"}, "example.com")

	// 验证 IP 被添加
	info1, exists := pool.GetIPInfo("1.1.1.1")
	if !exists {
		t.Fatal("IP 1.1.1.1 not found in pool")
	}
	if info1.RefCount != 1 {
		t.Errorf("Expected RefCount 1, got %d", info1.RefCount)
	}
	if info1.RepDomain != "example.com" {
		t.Errorf("Expected RepDomain example.com, got %s", info1.RepDomain)
	}

	info2, exists := pool.GetIPInfo("2.2.2.2")
	if !exists {
		t.Fatal("IP 2.2.2.2 not found in pool")
	}
	if info2.RefCount != 1 {
		t.Errorf("Expected RefCount 1, got %d", info2.RefCount)
	}

	// 测试更新 IP 列表（移除一个 IP，添加一个新 IP）
	pool.UpdateDomainIPs([]string{"1.1.1.1", "2.2.2.2"}, []string{"2.2.2.2", "3.3.3.3"}, "example.com")

	// 验证 1.1.1.1 被移除
	_, exists = pool.GetIPInfo("1.1.1.1")
	if exists {
		t.Fatal("IP 1.1.1.1 should be removed")
	}

	// 验证 2.2.2.2 引用计数保持为 1
	info2, exists = pool.GetIPInfo("2.2.2.2")
	if !exists {
		t.Fatal("IP 2.2.2.2 not found in pool")
	}
	if info2.RefCount != 1 {
		t.Errorf("Expected RefCount 1, got %d", info2.RefCount)
	}

	// 验证 3.3.3.3 被添加
	info3, exists := pool.GetIPInfo("3.3.3.3")
	if !exists {
		t.Fatal("IP 3.3.3.3 not found in pool")
	}
	if info3.RefCount != 1 {
		t.Errorf("Expected RefCount 1, got %d", info3.RefCount)
	}
}

func TestUpdateDomainIPs_MultipleDomains(t *testing.T) {
	pool := NewIPPool()

	// 两个域名共享同一个 IP
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1"}, "example.com")
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1"}, "test.com")

	// 验证引用计数为 2
	info, exists := pool.GetIPInfo("1.1.1.1")
	if !exists {
		t.Fatal("IP 1.1.1.1 not found in pool")
	}
	if info.RefCount != 2 {
		t.Errorf("Expected RefCount 2, got %d", info.RefCount)
	}

	// 一个域名移除该 IP
	pool.UpdateDomainIPs([]string{"1.1.1.1"}, []string{}, "example.com")

	// 验证引用计数降为 1
	info, exists = pool.GetIPInfo("1.1.1.1")
	if !exists {
		t.Fatal("IP 1.1.1.1 not found in pool")
	}
	if info.RefCount != 1 {
		t.Errorf("Expected RefCount 1, got %d", info.RefCount)
	}

	// 另一个域名也移除该 IP
	pool.UpdateDomainIPs([]string{"1.1.1.1"}, []string{}, "test.com")

	// 验证 IP 被完全移除
	_, exists = pool.GetIPInfo("1.1.1.1")
	if exists {
		t.Fatal("IP 1.1.1.1 should be removed when RefCount reaches 0")
	}
}

func TestRecordAccess(t *testing.T) {
	pool := NewIPPool()

	// 添加 IP
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1"}, "example.com")

	// 记录访问
	pool.RecordAccess("1.1.1.1", "example.com")

	// 验证访问热度增加
	info, exists := pool.GetIPInfo("1.1.1.1")
	if !exists {
		t.Fatal("IP 1.1.1.1 not found in pool")
	}
	if info.AccessHeat != 1 {
		t.Errorf("Expected AccessHeat 1, got %d", info.AccessHeat)
	}

	// 记录多次访问
	for i := 0; i < 5; i++ {
		pool.RecordAccess("1.1.1.1", "example.com")
	}

	info, exists = pool.GetIPInfo("1.1.1.1")
	if !exists {
		t.Fatal("IP 1.1.1.1 not found in pool")
	}
	if info.AccessHeat != 6 {
		t.Errorf("Expected AccessHeat 6, got %d", info.AccessHeat)
	}
}

func TestGetRepDomain(t *testing.T) {
	pool := NewIPPool()

	// 添加 IP
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1"}, "example.com")

	// 获取代表性域名
	repDomain, exists := pool.GetRepDomain("1.1.1.1")
	if !exists {
		t.Fatal("IP 1.1.1.1 not found in pool")
	}
	if repDomain != "example.com" {
		t.Errorf("Expected RepDomain example.com, got %s", repDomain)
	}

	// 测试不存在的 IP
	_, exists = pool.GetRepDomain("2.2.2.2")
	if exists {
		t.Fatal("Non-existent IP should not have a RepDomain")
	}
}

func TestGetAllIPs(t *testing.T) {
	pool := NewIPPool()

	// 添加多个 IP
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1", "2.2.2.2"}, "example.com")
	pool.UpdateDomainIPs([]string{}, []string{"3.3.3.3"}, "test.com")

	// 获取所有 IP
	allIPs := pool.GetAllIPs()
	if len(allIPs) != 3 {
		t.Errorf("Expected 3 IPs, got %d", len(allIPs))
	}

	// 验证返回的是副本，不是原始引用
	allIPs[0].RefCount = 999
	info, _ := pool.GetIPInfo(allIPs[0].IP)
	if info.RefCount == 999 {
		t.Fatal("GetAllIPs should return copies, not references")
	}
}

func TestGetStats(t *testing.T) {
	pool := NewIPPool()

	// 初始状态
	stats := pool.GetStats()
	if stats.TotalIPs != 0 {
		t.Errorf("Expected TotalIPs 0, got %d", stats.TotalIPs)
	}
	if stats.TotalRefCount != 0 {
		t.Errorf("Expected TotalRefCount 0, got %d", stats.TotalRefCount)
	}

	// 添加 IP
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1", "2.2.2.2"}, "example.com")
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1"}, "test.com")

	// 记录访问
	pool.RecordAccess("1.1.1.1", "example.com")
	pool.RecordAccess("2.2.2.2", "example.com")

	// 验证统计信息
	stats = pool.GetStats()
	if stats.TotalIPs != 2 {
		t.Errorf("Expected TotalIPs 2, got %d", stats.TotalIPs)
	}
	if stats.TotalRefCount != 3 {
		t.Errorf("Expected TotalRefCount 3, got %d", stats.TotalRefCount)
	}
	if stats.TotalHeat != 2 {
		t.Errorf("Expected TotalHeat 2, got %d", stats.TotalHeat)
	}
}

func TestClear(t *testing.T) {
	pool := NewIPPool()

	// 添加 IP
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1", "2.2.2.2"}, "example.com")

	// 清空
	pool.Clear()

	// 验证所有 IP 被移除
	_, exists := pool.GetIPInfo("1.1.1.1")
	if exists {
		t.Fatal("IP 1.1.1.1 should be removed after Clear")
	}

	_, exists = pool.GetIPInfo("2.2.2.2")
	if exists {
		t.Fatal("IP 2.2.2.2 should be removed after Clear")
	}

	// 验证统计信息被重置
	stats := pool.GetStats()
	if stats.TotalIPs != 0 {
		t.Errorf("Expected TotalIPs 0 after Clear, got %d", stats.TotalIPs)
	}
}

func TestRemoveIP(t *testing.T) {
	pool := NewIPPool()

	// 添加 IP
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1", "2.2.2.2"}, "example.com")

	// 移除一个 IP
	pool.RemoveIP("1.1.1.1")

	// 验证 IP 被移除
	_, exists := pool.GetIPInfo("1.1.1.1")
	if exists {
		t.Fatal("IP 1.1.1.1 should be removed")
	}

	// 验证另一个 IP 仍然存在
	_, exists = pool.GetIPInfo("2.2.2.2")
	if !exists {
		t.Fatal("IP 2.2.2.2 should still exist")
	}
}

func TestGetTopIPsByRefCount(t *testing.T) {
	pool := NewIPPool()

	// 添加 IP，设置不同的引用计数
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1"}, "example.com")
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1"}, "test.com")
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1"}, "demo.com")

	pool.UpdateDomainIPs([]string{}, []string{"2.2.2.2"}, "example.com")
	pool.UpdateDomainIPs([]string{}, []string{"2.2.2.2"}, "test.com")

	pool.UpdateDomainIPs([]string{}, []string{"3.3.3.3"}, "example.com")

	// 获取前 2 个 IP
	topIPs := pool.GetTopIPsByRefCount(2)
	if len(topIPs) != 2 {
		t.Errorf("Expected 2 IPs, got %d", len(topIPs))
	}

	// 验证排序正确（1.1.1.1 有 3 个引用，2.2.2.2 有 2 个引用）
	if topIPs[0].IP != "1.1.1.1" {
		t.Errorf("Expected first IP to be 1.1.1.1, got %s", topIPs[0].IP)
	}
	if topIPs[0].RefCount != 3 {
		t.Errorf("Expected RefCount 3, got %d", topIPs[0].RefCount)
	}

	if topIPs[1].IP != "2.2.2.2" {
		t.Errorf("Expected second IP to be 2.2.2.2, got %s", topIPs[1].IP)
	}
	if topIPs[1].RefCount != 2 {
		t.Errorf("Expected RefCount 2, got %d", topIPs[1].RefCount)
	}
}

func TestGetTopIPsByAccessHeat(t *testing.T) {
	pool := NewIPPool()

	// 添加 IP
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}, "example.com")

	// 记录不同次数的访问
	for i := 0; i < 10; i++ {
		pool.RecordAccess("1.1.1.1", "example.com")
	}
	for i := 0; i < 5; i++ {
		pool.RecordAccess("2.2.2.2", "example.com")
	}
	for i := 0; i < 2; i++ {
		pool.RecordAccess("3.3.3.3", "example.com")
	}

	// 获取前 2 个 IP
	topIPs := pool.GetTopIPsByAccessHeat(2)
	if len(topIPs) != 2 {
		t.Errorf("Expected 2 IPs, got %d", len(topIPs))
	}

	// 验证排序正确
	if topIPs[0].IP != "1.1.1.1" {
		t.Errorf("Expected first IP to be 1.1.1.1, got %s", topIPs[0].IP)
	}
	if topIPs[0].AccessHeat != 10 {
		t.Errorf("Expected AccessHeat 10, got %d", topIPs[0].AccessHeat)
	}

	if topIPs[1].IP != "2.2.2.2" {
		t.Errorf("Expected second IP to be 2.2.2.2, got %s", topIPs[1].IP)
	}
	if topIPs[1].AccessHeat != 5 {
		t.Errorf("Expected AccessHeat 5, got %d", topIPs[1].AccessHeat)
	}
}

func TestConcurrentAccess(t *testing.T) {
	pool := NewIPPool()
	var wg sync.WaitGroup

	// 并发添加 IP
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ip := "1.1.1." + string(rune('1'+n%10))
			domain := "example.com"
			pool.UpdateDomainIPs([]string{}, []string{ip}, domain)
		}(i)
	}

	// 并发记录访问
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ip := "1.1.1." + string(rune('1'+n%10))
			domain := "example.com"
			pool.RecordAccess(ip, domain)
		}(i)
	}

	wg.Wait()

	// 验证没有 panic 或数据损坏
	stats := pool.GetStats()
	if stats.TotalIPs == 0 {
		t.Error("Expected some IPs to be added")
	}
}

func TestLastAccessTime(t *testing.T) {
	pool := NewIPPool()

	// 添加 IP
	pool.UpdateDomainIPs([]string{}, []string{"1.1.1.1"}, "example.com")

	// 获取初始时间
	info1, _ := pool.GetIPInfo("1.1.1.1")
	initialTime := info1.LastAccess

	// 等待一小段时间
	time.Sleep(10 * time.Millisecond)

	// 记录访问
	pool.RecordAccess("1.1.1.1", "example.com")

	// 验证时间已更新
	info2, _ := pool.GetIPInfo("1.1.1.1")
	if !info2.LastAccess.After(initialTime) {
		t.Error("LastAccess time should be updated after RecordAccess")
	}
}
