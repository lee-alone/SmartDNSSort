package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"smartdnssort/config"
	"smartdnssort/dnsserver"
	"smartdnssort/stats"
	"smartdnssort/sysinstall"
	"smartdnssort/webapi"
	"time"
)

func main() {
	// 初始化随机数种子
	rand.Seed(time.Now().UnixNano())

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
	// 加载配置
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化统计模块
	s := stats.NewStats()

	// 启动 DNS 服务器
	dnsServer := dnsserver.NewServer(cfg, s)

	fmt.Printf("SmartDNSSort DNS Server started on port %d\n", cfg.DNS.ListenPort)
	fmt.Printf("Upstream servers: %v\n", cfg.Upstream.Servers)
	fmt.Printf("Ping concurrency: %d, timeout: %dms\n", cfg.Ping.Concurrency, cfg.Ping.TimeoutMs)

	// 启动 Web API 服务（可选）
	if cfg.WebUI.Enabled {
		webServer := webapi.NewServer(cfg, dnsServer.GetCache(), dnsServer)
		go func() {
			if err := webServer.Start(); err != nil {
				log.Printf("Web API server error: %v\n", err)
			}
		}()
	}

	if err := dnsServer.Start(); err != nil {
		log.Fatalf("Failed to start DNS server: %v", err)
	}
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
