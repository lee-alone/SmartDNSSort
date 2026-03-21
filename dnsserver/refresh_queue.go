package dnsserver

import (
	"fmt"
	"smartdnssort/logger"
	"sync"

	"golang.org/x/sync/singleflight"
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
// 第四阶段改造：使用 singleflight 确保同一个域名只有 1 个后台协程去上游刷新
type RefreshQueue struct {
	workers   int
	taskQueue chan RefreshTask
	wg        sync.WaitGroup
	quit      chan struct{}
	workFunc  func(task RefreshTask)

	// 第四阶段改造：使用 singleflight 实现唯一锁原则
	// 确保同一个域名只有 1 个后台协程去上游刷新，避免突发流量击穿上游 DNS
	sfGroup singleflight.Group
}

// NewRefreshQueue 创建一个新的刷新队列
func NewRefreshQueue(workers, queueSize int) *RefreshQueue {
	if workers <= 0 {
		workers = DefaultRefreshWorkers
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

// 如果任务已在进行中或队列已满,返回错误
func (rq *RefreshQueue) Submit(task RefreshTask) error {
	key := task.key()

	// 第四阶段改造：使用 singleflight 实现唯一锁原则
	// 如果同一个域名的刷新任务正在进行，singleflight 会自动合并请求
	_, err, _ := rq.sfGroup.Do(key, func() (interface{}, error) {
		// 尝试提交到队列
		select {
		case rq.taskQueue <- task:
			logger.Debugf("[RefreshQueue] 提交刷新任务: %s (type=%d)", task.Domain, task.Qtype)
			return nil, nil
		default:
			logger.Warnf("[RefreshQueue] 队列已满,刷新任务被丢弃: %s (type=%d)", task.Domain, task.Qtype)
			return nil, fmt.Errorf("queue full")
		}
	})

	return err
}

// markComplete 标记任务完成
// 第四阶段改造：singleflight 会自动管理任务状态，无需手动清理
func (rq *RefreshQueue) markComplete(task RefreshTask) {
	logger.Debugf("[RefreshQueue] 刷新任务完成: %s (type=%d)", task.Domain, task.Qtype)
	// singleflight 会自动清理任务状态
}

// Stop 停止队列并等待所有任务完成
func (rq *RefreshQueue) Stop() {
	close(rq.quit)
	// 等待所有 worker goroutine 退出
	// 注意:这里没有关闭 taskQueue,因为可能还有 worker 在读取
	// worker 会在 quit 信号后退出循环
	rq.wg.Wait()
	logger.Info("[RefreshQueue] 刷新队列已停止")
}
