package dnsserver

import (
	"log"
	"sync"
)

// RefreshTask 定义了缓存刷新任务
type RefreshTask struct {
	Domain string
	Qtype  uint16
}

// RefreshQueue 是一个用于处理缓存刷新任务的异步队列
type RefreshQueue struct {
	workers    int
	taskQueue  chan RefreshTask
	wg         sync.WaitGroup
	quit       chan struct{}
	workFunc   func(task RefreshTask)
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
		workers:   workers,
		taskQueue: make(chan RefreshTask, queueSize),
		quit:      make(chan struct{}),
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
						rq.workFunc(task)
					}
				case <-rq.quit:
					return
				}
			}
		}()
	}
}

// Submit 提交一个新任务到队列
// 如果队列已满，返回 false
func (rq *RefreshQueue) Submit(task RefreshTask) bool {
	select {
	case rq.taskQueue <- task:
		return true
	default:
		log.Printf("[RefreshQueue] 队列已满，刷新任务 %s (type %d) 被丢弃\n", task.Domain, task.Qtype)
		return false
	}
}

// Stop 停止队列并等待所有任务完成
func (rq *RefreshQueue) Stop() {
	close(rq.quit)
	// 等待所有 worker goroutine 退出
	// 注意：这里没有关闭 taskQueue，因为可能还有 worker 在读取
	// worker 会在 quit 信号后退出循环
	rq.wg.Wait()
	log.Println("[RefreshQueue] 刷新队列已停止")
}
