package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Wooo0/wan-manager/internal/api"
	"github.com/Wooo0/wan-manager/internal/collector"
	"github.com/Wooo0/wan-manager/internal/config"
	"github.com/Wooo0/wan-manager/internal/dpi"
	"github.com/Wooo0/wan-manager/internal/isp"
	ispdata "github.com/Wooo0/wan-manager/internal/rules/ispdata"
	"github.com/Wooo0/wan-manager/internal/routing"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "/etc/wan-manager/config.toml", "配置文件路径")
	routingConfigPath := flag.String("routing-config", "", "策略路由配置文件路径（默认与主配置同目录的 routing.toml）")
	showVersion := flag.Bool("version", false, "显示版本")
	forceDefaults := flag.Bool("force-defaults", false, "配置加载失败或关键字段（WAN 接口）缺失时强制使用默认配置")
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
		if *forceDefaults {
			log.Printf("加载配置失败，使用默认配置: %v", err)
			cfg = config.DefaultConfig()
		} else {
			log.Fatalf("加载配置失败（使用 --force-defaults 可强制使用默认配置）: %v", err)
		}
	}

	// 校验关键字段：WAN 接口缺失/为空时静默回退默认配置存在误操作风险
	if err := validateWANConfig(cfg); err != nil {
		if *forceDefaults {
			log.Printf("WAN 配置校验失败，使用默认 WAN 接口: %v", err)
			cfg.WAN = config.DefaultConfig().WAN
		} else {
			log.Fatalf("WAN 配置校验失败（使用 --force-defaults 可强制使用默认配置）: %v", err)
		}
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

	// 加载运营商 IP 段（远程最新优先，失败回退内置快照），合并到配置
	ispRes := ispdata.LoadDefaults()
	for _, e := range ispRes.Errors {
		log.Printf("运营商 IP 段加载警告: %v", e)
	}
	mergeISPData(routingCfg, ispRes.Data)
	if routingCfg.ISP.Enabled {
		log.Printf("运营商 IP 段: 电信 %d / 联通 %d / 移动 %d 条",
			len(routingCfg.ISP.Telecom), len(routingCfg.ISP.Unicom), len(routingCfg.ISP.Mobile))
	}

	// 初始化采集器
	wanCollector := collector.NewWANCollector(cfg.WAN, cfg.Collector.Interval)
	wanCollector.Start()

	clientCollector := collector.NewClientCollector(cfg.Collector.Interval, cfg.Collector.MiWiFi.Password)
	clientCollector.Start()

	// 初始化策略路由管理器
	wanNames := make([]string, len(cfg.WAN))
	for i, w := range cfg.WAN {
		wanNames[i] = w.Name
	}
	routingManager := routing.NewManager(routingCfg, wanNames)
	// 记录各运营商 IP 段的来源（远程/本地快照），供 Web 面板展示
	routingManager.SetISPDataSource(ispRes.Sources)

	// 解析游戏 .rules 目录：配置留空则用默认 rules/game（相对二进制），
	// 解析为绝对路径后交给管理器，避免相对工作目录的歧义。
	gameDir := routingCfg.GameRulesDir
	if gameDir == "" {
		gameDir = "rules/game"
	}
	gameDir = resolveDir(gameDir)
	routingManager.SetGameRulesDir(gameDir)
	log.Printf("游戏规则目录: %s", gameDir)

	// 启动识别一次：检测各 WAN 的运营商并建立 运营商->WAN 映射（不写死）
	ispDetector := isp.NewDetector()
	opWAN := detectISPWANMapping(cfg.WAN, routingCfg.ISP, ispDetector)
	routingManager.SetISPOperatorMap(opWAN)

	// 初始化 DPI 检测器：按配置选择系统级真实检测或 mock
	var dpiDetector dpi.Detector
	if cfg.DPI.Mode == "mock" {
		log.Printf("DPI: 使用 mock 检测器（非真实流量）")
		dpiDetector = dpi.NewMockDetector()
	} else {
		log.Printf("DPI: 使用系统级检测器（conntrack + %s 域名关联）", cfg.DPI.DNS.Source)
		dpiDetector = dpi.NewSystemDetector(cfg.DPI)
	}
	routingManager.SetDPIDetector(dpiDetector)

	// 启动 DPI 检测器（独立于策略路由开关，便于前端展示真实流）
	if err := dpiDetector.Start(); err != nil {
		log.Printf("DPI 检测器启动失败: %v", err)
	}

	if routingCfg.Enabled {
		if err := routingManager.Start(); err != nil {
			log.Printf("启动策略路由失败: %v", err)
		}
	}

	mux := http.NewServeMux()
	apiHandler := api.NewAPIHandler(wanCollector, clientCollector, routingManager, version)
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

	// 优雅关闭 HTTP 服务：等待活跃请求与 SSE 连接自然结束（最多 10s）
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP 服务关闭出错: %v", err)
	}

	// 停止 DPI 检测器
	if dpiDetector != nil {
		dpiDetector.Stop()
	}

	// 停止策略路由
	if routingCfg.Enabled {
		routingManager.Stop()
	}

	log.Println("服务已停止")
}

// validateWANConfig 校验关键字段：必须至少定义一个 WAN 接口，且每个接口的
// interface（实际网卡名）不能为空，否则后续建 ipset/iptables 会误操作。
func validateWANConfig(cfg *config.Config) error {
	if len(cfg.WAN) == 0 {
		return fmt.Errorf("未定义任何 WAN 接口")
	}
	for _, w := range cfg.WAN {
		if w.Interface == "" {
			return fmt.Errorf("WAN 接口 %q 未配置 interface 字段", w.Name)
		}
	}
	return nil
}

// mergeISPData 将加载到的运营商 IP 段合并进配置，保留用户在 [isp] 内联的自定义段（去重）。
func mergeISPData(cfg *routing.RoutingConfig, loaded map[string][]string) {
	mergeInto(&cfg.ISP.Telecom, loaded[ispdata.Telecom])
	mergeInto(&cfg.ISP.Unicom, loaded[ispdata.Unicom])
	mergeInto(&cfg.ISP.Mobile, loaded[ispdata.Mobile])
}

func mergeInto(dst *[]string, src []string) {
	seen := make(map[string]struct{}, len(*dst))
	for _, s := range *dst {
		seen[s] = struct{}{}
	}
	for _, s := range src {
		if _, ok := seen[s]; !ok {
			*dst = append(*dst, s)
			seen[s] = struct{}{}
		}
	}
}

// detectISPWANMapping 建立 运营商 -> WAN 名称 映射：
// 优先使用配置 wan_mapping 手动指定；否则（且 auto_detect=true）启动检测一次各 WAN 的运营商。
// 同一运营商命中多个 WAN 时取首个。
func detectISPWANMapping(wans []config.WANConfig, ispCfg routing.ISPConfig, det *isp.Detector) map[string]string {
	opWAN := map[string]string{}
	for _, w := range wans {
		op := ""
		if m, ok := ispCfg.WANMapping[w.Name]; ok {
			op = ispdata.NormalizeOperator(m)
		}
		if op == "" && ispCfg.AutoDetect {
			if info := det.Detect(w.Interface); info != nil {
				op = ispdata.NormalizeOperator(info.ISP)
			}
		}
		if op == "" {
			log.Printf("WAN %s (%s): 未识别到运营商，运营商分流将不包含此口", w.Name, w.Interface)
			continue
		}
		if _, exists := opWAN[op]; !exists {
			opWAN[op] = w.Name
		} else {
			log.Printf("运营商 %s 命中多个 WAN（%s, %s），使用首个 %s", op, opWAN[op], w.Name, opWAN[op])
		}
	}
	return opWAN
}

// resolveDir 将配置里的目录解析为绝对路径：已是绝对路径则直接用；
// 否则相对二进制所在目录解析（使 rules/game 等相对路径在任意工作目录下都稳定）。
func resolveDir(p string) string {
	if p == "" {
		return p
	}
	if filepath.IsAbs(p) {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return p
	}
	return filepath.Join(filepath.Dir(exe), p)
}
