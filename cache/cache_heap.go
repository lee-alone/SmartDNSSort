package cache

// expireEntry 过期堆中的条目
type expireEntry struct {
	key          string
	expiry       int64
	queryVersion int64 // 查询版本号，用于识别陈旧索引
}

// expireHeap 实现 container/heap.Interface
type expireHeap []expireEntry

func (h expireHeap) Len() int           { return len(h) }
func (h expireHeap) Less(i, j int) bool { return h[i].expiry < h[j].expiry }
func (h expireHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *expireHeap) Push(x interface{}) {
	*h = append(*h, x.(expireEntry))
}

func (h *expireHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// heapWorker 后台协程，负责异步维护过期堆
// 消除 Set 路径上的全局锁竞争
func (c *Cache) heapWorker() {
	defer c.heapWg.Done()

	for {
		select {
		case entry := <-c.addHeapChan:
			// 获取全局锁，添加到堆中
			c.mu.Lock()
			c.expiredHeap.Push(entry)
			c.mu.Unlock()

		case <-c.stopHeapChan:
			// 处理剩余的条目
			for {
				select {
				case entry := <-c.addHeapChan:
					c.mu.Lock()
					c.expiredHeap.Push(entry)
					c.mu.Unlock()
				default:
					return
				}
			}
		}
	}
}
