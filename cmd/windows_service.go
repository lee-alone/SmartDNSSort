//go:build windows
// +build windows

package main

import (
	"fmt"
	"os"
	"smartdnssort/config"
	"smartdnssort/dnsserver"
	"smartdnssort/logger"
	"smartdnssort/stats"
	"smartdnssort/sysinstall"
	"smartdnssort/webapi"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"
)

// isWindowsService 检查当前进程是否作为 Windows 服务运行
func isWindowsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isService
}

// windowsServiceHandler 实现 Windows 服务处理器
type windowsServiceHandler struct {
	dnsServer *dnsserver.Server
	webServer *webapi.Server
}

// Execute 实现 svc.Handler 接口
func (h *windowsServiceHandler) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending, Accepts: cmdsAccepted}

	// 启动服务
	if err := h.start(); err != nil {
		// 写入详细错误信息到日志文件
		logErrorToFile(fmt.Sprintf("服务启动失败: %v", err))
		changes <- svc.Status{State: svc.Stopped, Accepts: 0, Win32ExitCode: 1}
		return false, 1
	}

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted, Win32ExitCode: 0}

loop:
	for {
		c := <-r
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			h.stop()
			changes <- svc.Status{State: svc.Stopped, Accepts: 0, Win32ExitCode: 0}
			break loop
		}
	}

	return false, 0
}

// start 启动 DNS 和 Web 服务
func (h *windowsServiceHandler) start() error {
	// 设置工作目录为程序所在目录
	programDir := sysinstall.DefaultBinaryDir
	if programDir != "" {
		if err := os.Chdir(programDir); err != nil {
			logger.Warnf("切换到程序目录失败: %v", err)
		} else {
			logger.Infof("工作目录已设置为: %s", programDir)
		}
	}

	// 确保服务文件存在
	if err := sysinstall.EnsureServiceFiles(); err != nil {
		return fmt.Errorf("初始化服务文件失败: %v", err)
	}

	// 使用标准路径加载配置
	configPath := sysinstall.DefaultConfigPath()
	logger.Infof("尝试加载配置文件: %s", configPath)

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("加载配置失败: %v", err)
	}

	logger.SetLevel(cfg.System.LogLevel)
	logger.Infof("配置加载成功: %s", configPath)

	// 初始化统计模块
	s := stats.NewStats(&cfg.Stats)

	// 启动 DNS 服务器 (异步启动,避免阻塞 Windows 服务管理器)
	h.dnsServer = dnsserver.NewServer(cfg, s)
	go func() {
		if err := h.dnsServer.Start(); err != nil {
			logger.Errorf("启动 DNS 服务器失败: %v", err)
			logErrorToFile(fmt.Sprintf("运行 DNS 服务器发生错误: %v", err))
		}
	}()
	logger.Infof("DNS 服务器已作为后台任务启动,监听端口: %d", cfg.DNS.ListenPort)

	// 启动 Web UI
	if cfg.WebUI.Enabled {
		h.webServer = webapi.NewServer(cfg, h.dnsServer.GetCache(), h.dnsServer, configPath, nil)
		go func() {
			if err := h.webServer.Start(); err != nil {
				logger.Errorf("启动 Web UI 失败: %v", err)
			}
		}()
		logger.Infof("Web UI 已启动: http://localhost:%d", cfg.WebUI.ListenPort)
	}

	return nil
}

// stop 停止所有服务
func (h *windowsServiceHandler) stop() {
	logger.Info("正在停止所有服务...")

	var wg sync.WaitGroup

	if h.webServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.webServer.Stop()
		}()
	}

	if h.dnsServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.dnsServer.Shutdown()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("所有服务已停止")
	case <-time.After(3 * time.Second):
		logger.Warn("停止服务超时")
	}
}

// logErrorToFile 写入错误日志到文件,便于调试
func logErrorToFile(msg string) {
	logPath := "C:\\ProgramData\\SmartDNSSort\\logs\\service-error.log"
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	
	timestamp := fmt.Sprintf("[%s]", time.Now().Format("2006-01-02 15:04:05"))
	f.WriteString(fmt.Sprintf("%s %s\n", timestamp, msg))
}

// runAsWindowsService 以 Windows 服务模式运行
func runAsWindowsService() {
	handler := &windowsServiceHandler{}
	
	if err := svc.Run(sysinstall.ServiceName, handler); err != nil {
		fmt.Fprintf(os.Stderr, "Windows 服务运行失败: %v\n", err)
		os.Exit(1)
	}
}
