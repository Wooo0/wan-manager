package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Wooo0/wan-manager/internal/api"
	"github.com/Wooo0/wan-manager/internal/collector"
	"github.com/Wooo0/wan-manager/internal/config"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "/etc/wan-manager/config.toml", "配置文件路径")
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

	wanCollector := collector.NewWANCollector(cfg.WAN, cfg.Collector.Interval)
	wanCollector.Start()

	clientCollector := collector.NewClientCollector(cfg.Collector.Interval)
	clientCollector.Start()

	mux := http.NewServeMux()
	apiHandler := api.NewAPIHandler(wanCollector, clientCollector)
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
	log.Println("服务已停止")
}
