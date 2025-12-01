package cache

import (
	"context"
	"errors"
	"smartdnssort/logger"
	"sync"
	"sync/atomic"
	"time"
)

// SortTask 排序任务
type SortTask struct {
	Domain   string
	Qtype    uint16
	IPs      []string                                  // 待排序的 IP 列表
	TTL      uint32                                    // 上游 DNS 的原始 TTL
	Callback func(result *SortedCacheEntry, err error) // 排序完成回调
}

// SortQueue 异步排序任务队列
// 特点：
// 1. 支持对同一域名的并发请求加锁，避免重复排序
// 2. 支持配置工作线程数，限制并发排序任务
// 3. 使用事件驱动，排序完成后立即调用回调函数
type SortQueue struct {
	mu sync.Mutex

	// 排序任务队列
	taskQueue chan *SortTask

	// 工作线程数
	workers int

	// 停止信号
	stopCh chan struct{}
	stopWg sync.WaitGroup

	// 排序函数（由调用者提供）
	sortFunc func(ctx context.Context, ips []string) ([]string, []int, error)

	// 排序上下文超时时间
	sortTimeout time.Duration

	// 统计信息（原子操作）
	tasksProcessed int64
	tasksFailed    int64
}

// NewSortQueue 创建新的排序队列
// workers: 并发工作线程数
// queueSize: 任务队列缓冲大小（避免阻塞）
// sortTimeout: 单个排序任务的超时时间
func NewSortQueue(workers int, queueSize int, sortTimeout time.Duration) *SortQueue {
	if workers <= 0 {
		workers = 1
	}
	if queueSize <= 0 {
		queueSize = 100
	}
	if sortTimeout <= 0 {
		sortTimeout = 10 * time.Second
	}

	sq := &SortQueue{
		taskQueue:   make(chan *SortTask, queueSize),
		workers:     workers,
		stopCh:      make(chan struct{}),
		sortTimeout: sortTimeout,
	}

	// 启动工作线程
	sq.stopWg.Add(workers)
	for i := 0; i < workers; i++ {
		go sq.workerLoop(i)
	}

	return sq
}

// SetSortFunc 设置排序函数
// 排序函数接收 IP 列表，返回排序后的 IP、对应的 RTT 和错误信息
func (sq *SortQueue) SetSortFunc(fn func(ctx context.Context, ips []string) ([]string, []int, error)) {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	sq.sortFunc = fn
}

// Submit 提交一个排序任务
// 如果队列已满，返回 false，调用者可以重试或使用同步排序
func (sq *SortQueue) Submit(task *SortTask) bool {
	select {
	case sq.taskQueue <- task:
		return true
	case <-sq.stopCh:
		return false
	default:
		// 队列已满
		return false
	}
}

// SubmitBlocking 提交一个排序任务（阻塞直到任务入队）
func (sq *SortQueue) SubmitBlocking(task *SortTask) error {
	select {
	case sq.taskQueue <- task:
		return nil
	case <-sq.stopCh:
		return ErrQueueClosed
	}
}

// workerLoop 工作线程主循环
func (sq *SortQueue) workerLoop(id int) {
	defer sq.stopWg.Done()

	for {
		select {
		case task := <-sq.taskQueue:
			if task == nil {
				return
			}
			sq.processTask(task, id)

		case <-sq.stopCh:
			return
		}
	}
}

// processTask 处理单个排序任务
func (sq *SortQueue) processTask(task *SortTask, workerID int) {
	logger.Debugf("[SortQueue Worker #%d] 开始处理排序任务: %s (IP数量:%d)\n", workerID, task.Domain, len(task.IPs))

	sq.mu.Lock()
	sortFunc := sq.sortFunc
	timeout := sq.sortTimeout
	sq.mu.Unlock()

	if sortFunc == nil {
		logger.Debugf("[SortQueue Worker #%d] 错误：排序函数未设置\n", workerID)
		if task.Callback != nil {
			task.Callback(nil, ErrSortFuncNotSet)
		}
		return
	}

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 调用排序函数
	sortedIPs, rtts, err := sortFunc(ctx, task.IPs)
	if err != nil {
		logger.Warnf("[SortQueue Worker #%d] 排序失败: %s, 错误: %v\n", workerID, task.Domain, err)
		if task.Callback != nil {
			task.Callback(nil, err)
		}
		atomic.AddInt64(&sq.tasksFailed, 1)
		return
	}

	// 构造排序结果
	result := &SortedCacheEntry{
		IPs:       sortedIPs,
		RTTs:      rtts,
		Timestamp: time.Now(),
		TTL:       int(task.TTL), // 使用上游 DNS 的 TTL
		IsValid:   true,
	}

	logger.Debugf("[SortQueue Worker #%d] 排序完成: %s -> %v (RTT: %v)\n", workerID, task.Domain, sortedIPs, rtts)

	// 调用回调函数
	if task.Callback != nil {
		task.Callback(result, nil)
	}

	atomic.AddInt64(&sq.tasksProcessed, 1)
}

// Stop 停止排序队列（等待所有任务完成）
func (sq *SortQueue) Stop() {
	close(sq.stopCh)
	sq.stopWg.Wait()
	close(sq.taskQueue)
	processed := atomic.LoadInt64(&sq.tasksProcessed)
	failed := atomic.LoadInt64(&sq.tasksFailed)
	logger.Debugf("[SortQueue] 排序队列已停止. 已处理任务: %d, 失败任务: %d\n", processed, failed)
}

// GetStats 获取统计信息
func (sq *SortQueue) GetStats() (processed, failed int64) {
	processed = atomic.LoadInt64(&sq.tasksProcessed)
	failed = atomic.LoadInt64(&sq.tasksFailed)
	return
}

// 缓存的错误类型
var (
	ErrQueueClosed    = errors.New("sort queue is closed")
	ErrSortFuncNotSet = errors.New("sort function not set")
	ErrSortTimeout    = errors.New("sort operation timeout")
	ErrInvalidIPList  = errors.New("invalid IP list")
)
