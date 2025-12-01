package dnsserver

import (
	"fmt"
	"smartdnssort/logger"
	"sync"
)

// RefreshTask 定义了缓存刷新任务
type RefreshTask struct {
	Domain string
	Qtype  uint16
}

// taskKey 生成任务的唯一键
func (t RefreshTask) key() string {
	return fmt.Sprintf("%s#%d", t.Domain, t.Qtype)
}

// RefreshQueue 是一个用于处理缓存刷新任务的异步队列
type RefreshQueue struct {
	workers   int
	taskQueue chan RefreshTask
	wg        sync.WaitGroup
	quit      chan struct{}
	workFunc  func(task RefreshTask)

	// 去重机制:跟踪正在进行的刷新任务
	mu         sync.Mutex
	inProgress map[string]bool // key: domain#qtype
}

// NewRefreshQueue 创建一个新的刷新队列
func NewRefreshQueue(workers, queueSize int) *RefreshQueue {
	if workers <= 0 {
		workers = 4
	}
	if queueSize <= 0 {
		queueSize = 100
	}

	rq := &RefreshQueue{
		workers:    workers,
		taskQueue:  make(chan RefreshTask, queueSize),
		quit:       make(chan struct{}),
		inProgress: make(map[string]bool),
	}

	rq.start()
	return rq
}

// SetWorkFunc 设置要执行的工作函数
func (rq *RefreshQueue) SetWorkFunc(f func(task RefreshTask)) {
	rq.workFunc = f
}

// start 启动工作协程
func (rq *RefreshQueue) start() {
	rq.wg.Add(rq.workers)
	for i := 0; i < rq.workers; i++ {
		go func() {
			defer rq.wg.Done()
			for {
				select {
				case task, ok := <-rq.taskQueue:
					if !ok {
						return
					}
					if rq.workFunc != nil {
						// 执行工作函数
						rq.workFunc(task)
						// 任务完成后,清理进行中标记
						rq.markComplete(task)
					}
				case <-rq.quit:
					return
				}
			}
		}()
	}
}

// Submit 提交一个新任务到队列
// 如果任务已在进行中或队列已满,返回 false
func (rq *RefreshQueue) Submit(task RefreshTask) bool {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	key := task.key()

	// 检查是否已有相同任务在进行中
	if rq.inProgress[key] {
		logger.Debugf("[RefreshQueue] 刷新任务已在进行中,跳过重复提交: %s (type=%d)", task.Domain, task.Qtype)
		return false
	}

	// 尝试提交到队列
	select {
	case rq.taskQueue <- task:
		// 标记为进行中
		rq.inProgress[key] = true
		// 标记为进行中
		rq.inProgress[key] = true
		logger.Debugf("[RefreshQueue] 提交刷新任务: %s (type=%d)", task.Domain, task.Qtype)
		return true
	default:
		logger.Warnf("[RefreshQueue] 队列已满,刷新任务被丢弃: %s (type=%d)", task.Domain, task.Qtype)
		return false
	}
}

// markComplete 标记任务完成,清理进行中标记
func (rq *RefreshQueue) markComplete(task RefreshTask) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	key := task.key()
	delete(rq.inProgress, key)
	logger.Debugf("[RefreshQueue] 刷新任务完成: %s (type=%d)", task.Domain, task.Qtype)
}

// Stop 停止队列并等待所有任务完成
func (rq *RefreshQueue) Stop() {
	close(rq.quit)
	// 等待所有 worker goroutine 退出
	// 注意:这里没有关闭 taskQueue,因为可能还有 worker 在读取
	// worker 会在 quit 信号后退出循环
	rq.wg.Wait()
	// worker 会在 quit 信号后退出循环
	rq.wg.Wait()
	logger.Info("[RefreshQueue] 刷新队列已停止")
}
