package dnsserver

import (
	"fmt"

	"smartdnssort/logger"

	"github.com/miekg/dns"
)

// Start 启动 DNS 服务器
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.cfg.DNS.ListenPort)

	// 注册 DNS 处理函数
	dns.HandleFunc(".", s.handleQuery)

	// 启动 UDP 服务器
	s.udpServer = &dns.Server{
		Addr:    addr,
		Net:     "udp",
		Handler: dns.DefaultServeMux,
	}

	// 启动 TCP 服务器（如果启用）
	if s.cfg.DNS.EnableTCP {
		s.tcpServer = &dns.Server{
			Addr:    addr,
			Net:     "tcp",
			Handler: dns.DefaultServeMux,
		}

		go func() {
			logger.Infof("TCP DNS server started on %s", addr)
			if err := s.tcpServer.ListenAndServe(); err != nil {
				logger.Errorf("TCP server error: %v", err)
			}
		}()
	}

	// 启动清理过期缓存的 goroutine
	go s.cleanCacheRoutine()

	// 启动定期保存缓存的 goroutine
	go s.saveCacheRoutine()

	// Start the prefetcher
	s.prefetcher.Start()

	logger.Infof("UDP DNS server started on %s", addr)
	return s.udpServer.ListenAndServe()
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown() {
	logger.Info("[Server] 开始关闭服务器...")

	if s.udpServer != nil {
		if err := s.udpServer.Shutdown(); err != nil {
			logger.Errorf("[Server] UDP server shutdown error: %v", err)
		}
	}
	if s.tcpServer != nil {
		if err := s.tcpServer.Shutdown(); err != nil {
			logger.Errorf("[Server] TCP server shutdown error: %v", err)
		}
	}

	s.sortQueue.Stop()
	s.prefetcher.Stop()
	s.refreshQueue.Stop()
	logger.Info("[Server] 服务器已关闭")
}
