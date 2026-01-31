package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"smartdnssort/config"
	"smartdnssort/dnsserver"
	"smartdnssort/logger"
	"smartdnssort/stats"
	"smartdnssort/sysinstall"
	"smartdnssort/webapi"
	"syscall"
	"time"
)

func main() {
	// 定义命令行参数
	serviceCmd := flag.String("s", "", "系统服务管理命令 (install/uninstall/status)")
	configPath := flag.String("c", "config.yaml", "配置文件路径")
	workDir := flag.String("w", "", "工作目录")
	runUser := flag.String("user", "", "运行用户（仅限 install）")
	dryRun := flag.Bool("dry-run", false, "干运行模式，仅预览不执行（仅限 install/uninstall）")
	verbose := flag.Bool("v", false, "详细输出")
	help := flag.Bool("h", false, "显示帮助信息")

	flag.Parse()

	// 显示帮助信息
	if *help {
		printHelp()
		os.Exit(0)
	}

	// 处理系统服务命令（优先级最高）
	if *serviceCmd != "" {
		// 仅在 Linux 系统上支持
		if runtime.GOOS != "linux" {
			fmt.Fprintf(os.Stderr, "错误：系统服务管理仅在 Linux 系统上支持\n")
			os.Exit(1)
		}

		// 服务模式：强制使用标准绝对路径，忽略 -c 和 -w 参数
		cfg := sysinstall.InstallerConfig{
			ConfigPath: sysinstall.DefaultConfigPath(),
			WorkDir:    sysinstall.DefaultDataDir,
			RunUser:    *runUser,
			BinaryPath: sysinstall.DefaultBinaryPath(),
			DryRun:     *dryRun,
			Verbose:    *verbose,
		}

		installer := sysinstall.NewSystemInstaller(cfg)

		switch *serviceCmd {
		case "install":
			if err := installer.Install(); err != nil {
				fmt.Fprintf(os.Stderr, "错误：%v\n", err)
				os.Exit(1)
			}
		case "uninstall":
			if err := installer.Uninstall(); err != nil {
				fmt.Fprintf(os.Stderr, "错误：%v\n", err)
				os.Exit(1)
			}
		case "status":
			if err := installer.Status(); err != nil {
				fmt.Fprintf(os.Stderr, "错误：%v\n", err)
				os.Exit(1)
			}
		default:
			fmt.Fprintf(os.Stderr, "错误：未知的子命令 '%s'，支持的命令：install, uninstall, status\n", *serviceCmd)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// 独立运行模式：确定工作目录和配置文件路径
	effectiveWorkDir := *workDir
	if effectiveWorkDir == "" {
		var err error
		effectiveWorkDir, err = os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "错误：无法获取当前工作目录：%v\n", err)
			os.Exit(1)
		}
	}

	// 确定配置文件路径 (如果 -c 是相对路径，则与工作目录拼接)
	effectiveConfigPath := *configPath
	if !filepath.IsAbs(effectiveConfigPath) {
		effectiveConfigPath = filepath.Join(effectiveWorkDir, effectiveConfigPath)
	}

	// 正常的 DNS 服务器启动流程
	// 加载配置（先加载配置以获取日志级别设置）
	cfg, err := config.LoadConfig(effectiveConfigPath)
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	// 立即设置日志级别，确保后续所有日志都遵循配置
	logger.SetLevel(cfg.System.LogLevel)

	// 验证并修复配置文件
	logger.Infof("Validating config file: %s", effectiveConfigPath)
	if err := config.ValidateAndRepairConfig(effectiveConfigPath); err != nil {
		logger.Fatalf("Failed to validate/repair config: %v", err)
	}

	logger.Infof("Log level set to: %s", cfg.System.LogLevel)

	// 设置 GOMAXPROCS
	if cfg.System.MaxCPUCores > 0 {
		runtime.GOMAXPROCS(cfg.System.MaxCPUCores)
		logger.Infof("Set GOMAXPROCS to %d", cfg.System.MaxCPUCores)
	}

	// 初始化统计模块
	s := stats.NewStats(&cfg.Stats)
	defer s.Stop()

	// 启动 DNS 服务器
	dnsServer := dnsserver.NewServer(cfg, s)

	fmt.Printf("SmartDNSSort DNS Server started on port %d\n", cfg.DNS.ListenPort)
	fmt.Printf("Upstream servers: %v\n", cfg.Upstream.Servers)
	fmt.Printf("Ping concurrency: %d, timeout: %dms\n", cfg.Ping.Concurrency, cfg.Ping.TimeoutMs)

	// 启动 Web API 服务（可选）
	var webServer *webapi.Server
	webServerDone := make(chan error, 1)
	if cfg.WebUI.Enabled {
		// 定义重启回调函数
		restartFunc := func() {
			restartService(dnsServer, webServer)
		}

		webServer = webapi.NewServer(cfg, dnsServer.GetCache(), dnsServer, effectiveConfigPath, restartFunc)
		go func() {
			if err := webServer.Start(); err != nil {
				webServerDone <- err
			}
		}()
	}

	// 在 goroutine 中启动 DNS 服务器，以非阻塞方式运行
	dnsServerDone := make(chan error, 1)
	go func() {
		if err := dnsServer.Start(); err != nil {
			dnsServerDone <- err
		}
	}()

	// 设置优雅停机
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// 先关闭 Web 服务器
	if webServer != nil {
		logger.Info("Stopping Web API server...")
		if err := webServer.Stop(); err != nil {
			logger.Errorf("Failed to stop Web API server: %v", err)
		}
	}

	// 停止 DNS 服务
	dnsServer.Shutdown()

	// 等待 DNS 服务器完全停止
	select {
	case <-dnsServerDone:
		logger.Info("DNS server stopped.")
	case <-time.After(5 * time.Second):
		logger.Warn("DNS server shutdown timeout.")
	}

	logger.Info("Server gracefully stopped.")
}

func printHelp() {
	fmt.Print(`SmartDNSSort - 智能 DNS 排序服务器

使用方法：
  SmartDNSSort [选项]

选项：
  -s <子命令>      系统服务管理（仅 Linux）
		   - install    安装服务
		   - uninstall  卸载服务
		   - status     查看服务状态
  
  -c <路径>       配置文件路径（默认：config.yaml）
  -w <路径>       工作目录（默认：当前目录）
  -user <用户>    运行用户（仅限 install，默认：root）
  -dry-run        干运行模式，仅预览不执行（仅限 install/uninstall）
  -v              详细输出
  -h              显示此帮助信息

示例：
  # 启动 DNS 服务器
  SmartDNSSort -c /etc/SmartDNSSort/config.yaml

  # 安装系统服务
  sudo SmartDNSSort -s install

  # 预览安装流程
  sudo SmartDNSSort -s install --dry-run

  # 查看服务状态
  SmartDNSSort -s status

  # 卸载服务
  sudo SmartDNSSort -s uninstall
`)
}

func restartService(dnsServer *dnsserver.Server, webServer *webapi.Server) {
	logger.Info("Restarting service...")

	// Add a small delay to ensure configuration is flushed to disk
	time.Sleep(500 * time.Millisecond)

	// 1. 停止 Web 服务 (释放 8080 端口)
	if webServer != nil {
		logger.Info("Stopping Web API server...")
		if err := webServer.Stop(); err != nil {
			logger.Errorf("Failed to stop Web API server: %v", err)
		}
	}

	// Add delay between stopping services
	time.Sleep(500 * time.Millisecond)

	// 2. 停止 DNS 服务 (释放 53 端口)
	logger.Info("Stopping DNS server...")
	dnsServer.Shutdown()

	// Add delay after shutdown
	time.Sleep(500 * time.Millisecond)

	// 3. 检查是否为 systemd 服务 (仅 Linux)
	// systemd 会设置 INVOCATION_ID 环境变量
	if runtime.GOOS == "linux" && os.Getenv("INVOCATION_ID") != "" {
		logger.Info("Detected systemd environment. Exiting to trigger systemd restart...")
		os.Exit(0)
	}

	// 4. 手动重启 (Windows 或 Linux 手动运行)
	executable, err := os.Executable()
	if err != nil {
		logger.Fatalf("Failed to get executable path: %v", err)
	}

	logger.Infof("Spawning new process: %s %v", executable, os.Args[1:])

	// 启动新进程
	cmd := exec.Command(executable, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		logger.Fatalf("Failed to start new process: %v", err)
	}

	logger.Info("New process started. Exiting current process...")
	os.Exit(0)
}
