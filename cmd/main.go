package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"smartdnssort/config"
	"smartdnssort/dnsserver"
	"smartdnssort/stats"
	"smartdnssort/sysinstall"
	"smartdnssort/webapi"
	"syscall"
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

	// 处理系统服务命令
	if *serviceCmd != "" {
		// 仅在 Linux 系统上支持
		if runtime.GOOS != "linux" {
			fmt.Fprintf(os.Stderr, "错误：系统服务管理仅在 Linux 系统上支持\n")
			os.Exit(1)
		}

		cfg := sysinstall.InstallerConfig{
			ConfigPath: *configPath,
			WorkDir:    *workDir,
			RunUser:    *runUser,
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

	// 正常的 DNS 服务器启动流程
	// 验证并修复配置文件
	log.Printf("Validating config file: %s\n", *configPath)
	if err := config.ValidateAndRepairConfig(*configPath); err != nil {
		log.Fatalf("Failed to validate/repair config: %v", err)
	}

	// 加载配置
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 设置 GOMAXPROCS
	if cfg.System.MaxCPUCores > 0 {
		runtime.GOMAXPROCS(cfg.System.MaxCPUCores)
		log.Printf("Set GOMAXPROCS to %d\n", cfg.System.MaxCPUCores)
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
	if cfg.WebUI.Enabled {
		var webServer *webapi.Server

		// 定义重启回调函数
		restartFunc := func() {
			restartService(dnsServer, webServer)
		}

		webServer = webapi.NewServer(cfg, dnsServer.GetCache(), dnsServer, *configPath, restartFunc)
		go func() {
			if err := webServer.Start(); err != nil {
				log.Printf("Web API server error: %v\n", err)
			}
		}()
	}

	// 在 goroutine 中启动 DNS 服务器，以非阻塞方式运行
	go func() {
		if err := dnsServer.Start(); err != nil {
			log.Fatalf("Failed to start DNS server: %v", err)
		}
	}()

	// 设置优雅停机
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// 停止服务
	dnsServer.Shutdown()

	log.Println("Server gracefully stopped.")
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
  sudo SmartDNSSort -s install -c /etc/SmartDNSSort/config.yaml

  # 预览安装流程
  sudo SmartDNSSort -s install --dry-run

  # 查看服务状态
  SmartDNSSort -s status

  # 卸载服务
  sudo SmartDNSSort -s uninstall
`)
}

func restartService(dnsServer *dnsserver.Server, webServer *webapi.Server) {
	log.Println("Restarting service...")

	// 1. 停止 Web 服务 (释放 8080 端口)
	if webServer != nil {
		log.Println("Stopping Web API server...")
		if err := webServer.Stop(); err != nil {
			log.Printf("Failed to stop Web API server: %v", err)
		}
	}

	// 2. 停止 DNS 服务 (释放 53 端口)
	log.Println("Stopping DNS server...")
	dnsServer.Shutdown()

	// 3. 检查是否为 systemd 服务 (仅 Linux)
	// systemd 会设置 INVOCATION_ID 环境变量
	if runtime.GOOS == "linux" && os.Getenv("INVOCATION_ID") != "" {
		log.Println("Detected systemd environment. Exiting to trigger systemd restart...")
		os.Exit(0)
	}

	// 4. 手动重启 (Windows 或 Linux 手动运行)
	executable, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get executable path: %v", err)
		os.Exit(1)
	}

	log.Printf("Spawning new process: %s %v", executable, os.Args[1:])

	// 启动新进程
	cmd := exec.Command(executable, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start new process: %v", err)
		// 如果启动失败，我们仍然退出，因为旧服务已经停止了
		os.Exit(1)
	}

	log.Println("New process started. Exiting current process...")
	os.Exit(0)
}
