package resolver

import (
	"context"
	"fmt"
	"net"
	"smartdnssort/config"
	"smartdnssort/logger"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Server DNS 服务器
type Server struct {
	config   *config.RecursiveConfig
	resolver *Resolver
	listener net.Listener
	udpConn  *net.UDPConn
	mu       sync.RWMutex
	running  bool
	done     chan struct{}
}

// NewServer 创建新的 DNS 服务器
func NewServer(cfg *config.RecursiveConfig, rootHints []string) (*Server, error) {
	if cfg == nil {
		logger.Error("server config is nil")
		return nil, fmt.Errorf("config is nil")
	}

	logger.Infof("creating Recursive DNS server on port %d", cfg.Port)

	// 创建递归解析器
	resolver, err := NewResolver(cfg, rootHints)
	if err != nil {
		logger.Errorf("failed to create resolver: %v", err)
		return nil, fmt.Errorf("failed to create resolver: %w", err)
	}

	logger.Infof("Recursive DNS server created successfully")
	return &Server{
		config:   cfg,
		resolver: resolver,
		done:     make(chan struct{}),
	}, nil
}

// Start 启动 DNS 服务器
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		logger.Warn("server is already running")
		return fmt.Errorf("server is already running")
	}

	logger.Infof("starting Recursive DNS server on :%d", s.config.Port)

	// 创建 UDP 监听器
	addr := fmt.Sprintf("127.0.0.1:%d", s.config.Port)
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}
	s.udpConn = udpConn

	// 创建 TCP 监听器
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return err
	}
	tcpListener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		return err
	}
	s.listener = tcpListener

	s.running = true

	// 启动收发 goroutine
	go s.serveUDP()
	go s.serveTCP()

	return nil
}

// Stop 停止 DNS 服务器
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		logger.Warn("server is not running")
		return fmt.Errorf("server is not running")
	}

	logger.Info("stopping DNS server")

	s.running = false
	close(s.done)

	if s.udpConn != nil {
		s.udpConn.Close()
	}

	if s.listener != nil {
		s.listener.Close()
	}

	if s.resolver != nil {
		s.resolver.Close()
	}

	logger.Info("Recursive DNS server stopped successfully")
	return nil
}

// serveUDP 处理 UDP 请求
func (s *Server) serveUDP() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-s.done:
			return
		default:
		}

		s.udpConn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := s.udpConn.ReadFromUDP(buf)
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}
			if !s.running {
				return
			}
			logger.Errorf("UDP read error: %v", err)
			continue
		}

		go func(data []byte, clientAddr *net.UDPAddr) {
			msg := new(dns.Msg)
			if err := msg.Unpack(data); err != nil {
				return
			}
			resp := s.handleQuery(msg)
			out, err := resp.Pack()
			if err != nil {
				return
			}
			s.udpConn.WriteToUDP(out, clientAddr)
		}(append([]byte(nil), buf[:n]...), addr)
	}
}

// serveTCP 处理 TCP 请求
func (s *Server) serveTCP() {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		s.listener.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))
		conn, err := s.listener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				continue
			}
			if !s.running {
				return
			}
			continue
		}
		go s.handleConnection(conn)
	}
}

// handleConnection 处理 TCP 连接
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// 设置超时
	timeout := time.Duration(s.config.QueryTimeout) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	for {
		s.mu.RLock()
		if !s.running {
			s.mu.RUnlock()
			return
		}
		s.mu.RUnlock()

		conn.SetReadDeadline(time.Now().Add(timeout))

		// TCP DNS 报文前有两个字节的长度
		lenBuf := make([]byte, 2)
		_, err := conn.Read(lenBuf)
		if err != nil {
			return
		}
		length := uint16(lenBuf[0])<<8 | uint16(lenBuf[1])

		msgBuf := make([]byte, length)
		_, err = conn.Read(msgBuf)
		if err != nil {
			return
		}

		msg := new(dns.Msg)
		if err := msg.Unpack(msgBuf); err != nil {
			return
		}

		resp := s.handleQuery(msg)
		out, err := resp.Pack()
		if err != nil {
			return
		}

		// 发送长度然后再发送报文
		resLen := uint16(len(out))
		resLenBuf := []byte{byte(resLen >> 8), byte(resLen & 0xff)}
		conn.Write(resLenBuf)
		conn.Write(out)
	}
}

// handleQuery 处理 DNS 查询
func (s *Server) handleQuery(msg *dns.Msg) *dns.Msg {
	response := &dns.Msg{}
	response.SetReply(msg)

	if len(msg.Question) == 0 {
		response.SetRcode(msg, dns.RcodeFormatError)
		return response
	}

	// 处理每个问题
	for _, q := range msg.Question {
		// 执行递归查询
		timeout := time.Duration(s.config.QueryTimeout) * time.Millisecond
		if timeout == 0 {
			timeout = 5 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		rrs, err := s.resolver.Resolve(ctx, q.Name, q.Qtype)
		if err != nil {
			response.SetRcode(msg, dns.RcodeServerFailure)
			continue
		}

		response.Answer = append(response.Answer, rrs...)
	}

	return response
}

// GetStats 获取统计信息
func (s *Server) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]interface{}{
		"running": s.running,
	}

	if s.resolver != nil {
		stats["resolver"] = s.resolver.GetStats()
	}

	return stats
}

// IsRunning 检查服务器是否运行中
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.running
}

// GetResolver 获取递归解析器
func (s *Server) GetResolver() *Resolver {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.resolver
}
