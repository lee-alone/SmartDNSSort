//go:build windows
// +build windows

package sysinstall

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsSERVICE_WIN32_OWN_PROCESS = 0x00000010
)

// IsWindowsService 检查当前进程是否作为 Windows 服务运行
func IsWindowsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isService
}

// RunAsService 以 Windows 服务模式运行
func RunAsService(handler svc.Handler) error {
	return svc.Run(ServiceName, handler)
}

// InstallServiceWithMgr 使用 Windows Service Manager API 安装服务
func (si *SystemInstaller) InstallServiceWithMgr() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将通过 SCM 安装服务：%s\n", ServiceName)
		return nil
	}

	si.log("通过 Windows SCM 安装服务：%s", ServiceName)

	// 连接到服务控制管理器
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接服务控制管理器失败: %v", err)
	}
	defer m.Disconnect()

	// 检查服务是否已存在
	oldService, err := m.OpenService(ServiceName)
	if err == nil {
		// 服务已存在,先尝试停止
		si.log("服务已存在,先停止并删除旧服务")
		
		// 尝试停止
		oldStatus, err := oldService.Query()
		if err == nil && oldStatus.State == svc.Running {
			si.log("旧服务正在运行,正在停止...")
			_, _ = oldService.Control(svc.Stop)
			time.Sleep(2 * time.Second)
		}
		
		// 删除旧服务
		if err := oldService.Delete(); err != nil {
			oldService.Close()
			return fmt.Errorf("删除旧服务失败: %v (请先手动卸载: sc delete %s)", err, ServiceName)
		}
		oldService.Close()
		
		// 等待服务删除完成
		si.log("等待旧服务删除...")
		time.Sleep(2 * time.Second)
	}

	// 获取二进制路径
	binaryPath := DefaultBinaryPath()

	// 构建完整命令行参数
	// 服务启动时不传递 -c 参数,程序会通过标准路径自动加载配置
	// 注意:路径必须用引号包裹,因为包含空格
	cmdLine := fmt.Sprintf(`"%s"`, binaryPath)

	si.log("服务命令行: %s", cmdLine)
	
	// 打印详细信息用于调试
	si.log("二进制路径: %s", binaryPath)
	si.log("配置路径: %s", DefaultConfigPath())

	// 创建服务配置
	config := mgr.Config{
		ServiceType:  windowsSERVICE_WIN32_OWN_PROCESS,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
		DisplayName:  "SmartDNSSort DNS Server",
		Description:  ServiceDesc,
	}

	// 创建服务 (不添加依赖项,避免启动问题)
	s, err := m.CreateService(ServiceName, cmdLine, config)
	if err != nil {
		return fmt.Errorf("创建服务失败: %v", err)
	}
	defer s.Close()

	// 配置服务以 LocalSystem 账户运行
	// 需要设置 SERVICE_ALL_ACCESS 权限
	if err := updateServiceConfig(s); err != nil {
		si.log("警告: 更新服务配置失败: %v", err)
	}

	si.log("服务创建成功")
	return nil
}

// updateServiceConfig 更新服务配置,设置正确的账户和工作目录
func updateServiceConfig(_ *mgr.Service) error {
	// 当前通过 sc config 命令设置服务账户,此函数保留用于未来扩展
	return nil
}

// StartServiceWithMgr 使用 Windows Service Manager API 启动服务
func (si *SystemInstaller) StartServiceWithMgr() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将通过 SCM 启动服务：%s\n", ServiceName)
		return nil
	}

	si.log("通过 Windows SCM 启动服务：%s", ServiceName)

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接服务控制管理器失败: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("打开服务失败: %v", err)
	}
	defer s.Close()

	// 启动服务
	err = s.Start()
	if err != nil {
		return fmt.Errorf("启动服务失败: %v", err)
	}

	// 等待服务进入运行状态
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		status, err := s.Query()
		if err != nil {
			continue
		}
		if status.State == svc.Running {
			si.log("服务启动成功")
			return nil
		}
		if status.State == svc.Stopped {
			return fmt.Errorf("服务启动后停止,请检查日志")
		}
	}

	return fmt.Errorf("服务启动超时")
}

// StopServiceWithMgr 使用 Windows Service Manager API 停止服务
func (si *SystemInstaller) StopServiceWithMgr() error {
	if si.config.DryRun {
		fmt.Printf("[DRY-RUN] 将通过 SCM 停止服务：%s\n", ServiceName)
		return nil
	}

	si.log("通过 Windows SCM 停止服务：%s", ServiceName)

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接服务控制管理器失败: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return fmt.Errorf("打开服务失败: %v", err)
	}
	defer s.Close()

	_, err = s.Control(svc.Stop)
	if err != nil {
		// 如果服务未运行,忽略错误
		return fmt.Errorf("停止服务失败: %v", err)
	}

	// 等待服务停止
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		status, err := s.Query()
		if err != nil {
			continue
		}
		if status.State == svc.Stopped {
			si.log("服务停止成功")
			return nil
		}
	}

	return fmt.Errorf("服务停止超时")
}

// GetServiceStatusWithMgr 使用 Windows Service Manager API 获取服务状态
func (si *SystemInstaller) GetServiceStatusWithMgr() (string, error) {
	m, err := mgr.Connect()
	if err != nil {
		return "UNKNOWN", fmt.Errorf("连接服务控制管理器失败: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(ServiceName)
	if err != nil {
		return "NOT_FOUND", nil
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return "UNKNOWN", err
	}

	switch status.State {
	case svc.Running:
		return "RUNNING", nil
	case svc.Stopped:
		return "STOPPED", nil
	case svc.StartPending:
		return "START_PENDING", nil
	case svc.StopPending:
		return "STOP_PENDING", nil
	case svc.Paused:
		return "PAUSED", nil
	case svc.ContinuePending:
		return "CONTINUE_PENDING", nil
	case svc.PausePending:
		return "PAUSE_PENDING", nil
	}

	return "UNKNOWN", nil
}

// EnsureServiceDirs 确保服务所需的所有目录存在
func EnsureServiceDirs() error {
	dirs := []string{
		DefaultConfigDir,
		DefaultDataDir,
		DefaultLogDir,
		DefaultBinaryDir,
		DefaultWebDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %v", dir, err)
		}
	}

	return nil
}

// GenerateServiceConfigIfMissing 如果配置文件不存在则生成默认配置
func GenerateServiceConfigIfMissing() error {
	configPath := DefaultConfigPath()

	// 检查文件是否已存在
	if _, err := os.Stat(configPath); err == nil {
		return nil // 配置文件已存在
	}

	// 确保配置目录存在
	if err := os.MkdirAll(DefaultConfigDir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %v", err)
	}

	// 生成默认配置
	defaultConfig := `# SmartDNSSort 配置文件
# 生成的默认配置与手动安装时相同

# DNS 服务器配置
dns:
  listen_port: 53
  enable_tcp: true
  enable_ipv6: true

# 上游 DNS 服务器配置
upstream:
  servers:
    - "192.168.1.10"
    - "192.168.1.11"
    - "192.168.1.25"
  strategy: "random"
  timeout_ms: 3000
  concurrency: 4

# Ping 检测配置
ping:
  count: 3
  timeout_ms: 500
  concurrency: 16
  strategy: "min"

# DNS 缓存配置
cache:
  min_ttl_seconds: 3600
  max_ttl_seconds: 84600

# Web UI 管理界面配置
webui:
  enabled: true
  listen_port: 8080
`

	if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}

	return nil
}

// EnsureServiceFiles 确保所有必要的文件都存在
func EnsureServiceFiles() error {
	// 确保目录存在
	if err := EnsureServiceDirs(); err != nil {
		return err
	}

	// 确保配置文件存在
	if err := GenerateServiceConfigIfMissing(); err != nil {
		return err
	}

	return nil
}
