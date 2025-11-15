package main

import (
	"fmt"
	"log"
	"math/rand"
	"smartdnssort/config"
	"smartdnssort/dnsserver"
	"smartdnssort/stats"
	"smartdnssort/webapi"
	"time"
)

func main() {
	// 初始化随机数种子
	rand.Seed(time.Now().UnixNano())

	// 加载配置
	cfg, err := config.LoadConfig("config.yaml")
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
