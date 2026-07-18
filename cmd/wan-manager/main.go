package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Wooo0/wan-manager/internal/api"
	"github.com/Wooo0/wan-manager/internal/collector"
	"github.com/Wooo0/wan-manager/internal/config"
	"github.com/Wooo0/wan-manager/internal/dpi"
	"github.com/Wooo0/wan-manager/internal/routing"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "/etc/wan-manager/config.toml", "配置文件路径")
	routingConfigPath := flag.String("routing-config", "", "策略路由配置文件路径（默认与主配置同目录的 routing.toml）")
	showVersion := flag.Bool("version", false, "显示版本")
	flag.Parse()

	// 兼容 --version（Go flag 包默认只识别 -version，这里手动转换）
	for _, arg := range os.Args[1:] {
		if arg == "--version" {
			*showVersion = true
			break
		}
	}

	if *showVersion {
		fmt.Printf("wan-manager %s\n", version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("加载配置失败: %v, 使用默认配置", err)
		cfg = config.DefaultConfig()
	}

	log.Printf("wan-manager %s 启动", version)
	log.Printf("监听地址: %s", cfg.Server.ListenAddr)
	log.Printf("采集间隔: %d 秒", cfg.Collector.Interval)
	log.Printf("WAN 接口数量: %d", len(cfg.WAN))
	for _, w := range cfg.WAN {
		log.Printf("  - %s (%s): %s", w.Name, w.Label, w.Interface)
	}

	// 加载策略路由配置
	if *routingConfigPath == "" {
		// 默认与主配置同目录
		*routingConfigPath = filepath.Join(filepath.Dir(*configPath), "routing.toml")
	}
	routingCfg, err := routing.LoadRoutingConfig(*routingConfigPath)
	if err != nil {
		log.Printf("加载路由配置失败: %v, 使用默认配置", err)
		routingCfg = routing.DefaultRoutingConfig()
	}
	log.Printf("策略路由: enabled=%v, default_wan=%s, rules=%d", routingCfg.Enabled, routingCfg.DefaultWAN, len(routingCfg.Rules))

	// 初始化采集器
	wanCollector := collector.NewWANCollector(cfg.WAN, cfg.Collector.Interval)
	wanCollector.Start()

	clientCollector := collector.NewClientCollector(cfg.Collector.Interval)
	clientCollector.Start()

	// 初始化策略路由管理器
	wanNames := make([]string, len(cfg.WAN))
	for i, w := range cfg.WAN {
		wanNames[i] = w.Name
	}
	routingManager := routing.NewManager(routingCfg, wanNames)

	// 初始化 DPI 检测器（开发环境使用 mock，生产环境替换为真实 nDPI）
	dpiDetector := dpi.NewMockDetector()
	routingManager.SetDPIDetector(dpiDetector)

	if routingCfg.Enabled {
		if err := routingManager.Start(); err != nil {
			log.Printf("启动策略路由失败: %v", err)
		}
	}

	mux := http.NewServeMux()
	apiHandler := api.NewAPIHandler(wanCollector, clientCollector, routingManager)
	apiHandler.RegisterRoutes(mux)

	server := &http.Server{
		Addr:    cfg.Server.ListenAddr,
		Handler: mux,
	}

	go func() {
		log.Printf("HTTP 服务启动: %s", cfg.Server.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP 服务错误: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("收到退出信号，正在关闭...")

	// 停止策略路由
	if routingCfg.Enabled {
		routingManager.Stop()
	}

	log.Println("服务已停止")
}
