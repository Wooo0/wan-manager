package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Wooo0/wan-manager/internal/collector"
	"github.com/Wooo0/wan-manager/internal/dpi"
	"github.com/Wooo0/wan-manager/internal/rules"
	"github.com/Wooo0/wan-manager/internal/routing"
	"github.com/Wooo0/wan-manager/internal/web"
)

// APIHandler API 处理器
type APIHandler struct {
	wanCollector    *collector.WANCollector
	clientCollector *collector.ClientCollector
	routingManager  *routing.Manager
	version         string
}

// SummaryResponse 汇总响应
type SummaryResponse struct {
	WANS     []collector.WANStats   `json:"wans"`
	Clients  []collector.ClientInfo `json:"clients"`
	Routing  *RoutingStatus         `json:"routing,omitempty"`
	UpdateAt time.Time              `json:"update_at"`
}

// RoutingStatus 路由状态
type RoutingStatus struct {
	Enabled bool                   `json:"enabled"`
	Active  bool                   `json:"active"`
	Config  *routing.RoutingConfig `json:"config,omitempty"`
}

// NewAPIHandler 创建 API 处理器
func NewAPIHandler(wc *collector.WANCollector, cc *collector.ClientCollector, rm *routing.Manager, version string) *APIHandler {
	return &APIHandler{
		wanCollector:    wc,
		clientCollector: cc,
		routingManager:  rm,
		version:         version,
	}
}

// RegisterRoutes 注册路由
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	// Web 界面
	mux.HandleFunc("/", h.handleRoot)

	// API 接口
	mux.HandleFunc("/api/v1/version", h.handleVersion)
	mux.HandleFunc("/api/v1/summary", h.handleSummary)
	mux.HandleFunc("/api/v1/wan", h.handleWAN)
	mux.HandleFunc("/api/v1/clients", h.handleClients)
	mux.HandleFunc("/api/v1/health", h.handleHealth)
	mux.HandleFunc("/api/v1/routing", h.handleRouting)
	mux.HandleFunc("/api/v1/apps/catalog", h.handleAppCatalog)
	mux.HandleFunc("/api/v1/dpi/flows", h.handleDPIFlows)
	mux.HandleFunc("/api/v1/isp/logo", h.handleISPLogo)
	mux.HandleFunc("/api/v1/game-library", h.handleGameLibrary)
	mux.HandleFunc("/api/v1/events", h.handleEvents)
}

// handleVersion 返回版本信息
func (h *APIHandler) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{
		"version": h.version,
	})
}

// handleRoot 根路径返回 Web 管理界面
func (h *APIHandler) handleRoot(w http.ResponseWriter, r *http.Request) {
	// 只处理精确匹配的根路径
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// 如果请求 JSON，返回 API 信息
	if r.Header.Get("Accept") == "application/json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":    "wan-manager",
			"version": h.version,
			"endpoints": []string{
				"/api/v1/health",
				"/api/v1/summary",
				"/api/v1/wan",
				"/api/v1/clients",
				"/api/v1/routing",
			},
		})
		return
	}

	// 返回 Web 界面
	html, err := web.IndexHTML()
	if err != nil {
		http.Error(w, "加载页面失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(html)
}

func (h *APIHandler) handleSummary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	resp := SummaryResponse{
		WANS:     h.wanCollector.GetStats(),
		Clients:  h.clientCollector.GetClients(),
		UpdateAt: time.Now(),
	}

	// 添加路由状态
	if h.routingManager != nil {
		resp.Routing = &RoutingStatus{
			Enabled: h.routingManager.GetConfig().Enabled,
			Active:  h.routingManager.IsActive(),
		}
	}

	json.NewEncoder(w).Encode(resp)
}

func (h *APIHandler) handleWAN(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(h.wanCollector.GetStats())
}

func (h *APIHandler) handleClients(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(h.clientCollector.GetClients())
}

func (h *APIHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"time":   time.Now(),
	})
}

// handleRouting 处理路由相关请求
func (h *APIHandler) handleRouting(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	switch r.Method {
	case "GET":
		h.handleGetRouting(w, r)
	case "POST":
		h.handlePostRouting(w, r)
	case "PUT":
		h.handlePutRouting(w, r)
	case "DELETE":
		h.handleDeleteRouting(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// handleGetRouting 获取路由配置和状态
func (h *APIHandler) handleGetRouting(w http.ResponseWriter, r *http.Request) {
	result := map[string]interface{}{
		"enabled": false,
		"active":  false,
	}

	mwan3Config, err := routing.LoadMWAN3Config("")
	if err == nil && len(mwan3Config.Interfaces) > 0 {
		result["mwan3_enabled"] = true
		result["mwan3"] = mwan3Config

		wan1Weight, wan2Weight := routing.GetWAN1WAN2Ratio(mwan3Config)
		result["balance_ratio"] = map[string]interface{}{
			"wan1":    wan1Weight,
			"wan2":    wan2Weight,
			"display": fmt.Sprintf("%d:%d", wan1Weight, wan2Weight),
		}

		// 返回所有 mwan3 规则（包括默认策略规则）
		result["mwan3_rules"] = mwan3Config.Rules

		// 同时返回解析后的自定义规则（用于兼容）
		ipRules := routing.ParseIPRulesFromMWAN3(mwan3Config)
		result["custom_rules"] = ipRules
	} else {
		result["mwan3_enabled"] = false
		result["mwan3_rules"] = []interface{}{}
	}

	if h.routingManager != nil {
		// 返回配置深拷贝，避免直接修改管理器持有的共享 config 指针
		// （无锁并发读写，与 Reload/其他读取构成 data race）。
		cfgCopy := h.routingManager.GetConfigCopy()
		result["enabled"] = cfgCopy.Enabled
		result["active"] = h.routingManager.IsActive()
		if cfgCopy.MWAN3Config == nil && mwan3Config != nil {
			cfgCopy.MWAN3Config = mwan3Config
		}
		result["config"] = cfgCopy
	}

	json.NewEncoder(w).Encode(result)
}

// handlePostRouting 添加路由规则或切换策略路由开关
func (h *APIHandler) handlePostRouting(w http.ResponseWriter, r *http.Request) {
	if h.routingManager == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "路由管理器未初始化",
		})
		return
	}

	// 先解析为通用 map，判断是开关切换还是添加规则
	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "请求参数格式错误",
		})
		return
	}

	// 如果有 enabled 字段但没有 name 字段，说明是开关切换
	if enabledVal, hasEnabled := raw["enabled"]; hasEnabled {
		if _, hasName := raw["name"]; !hasName {
			enabled, ok := enabledVal.(bool)
			if !ok {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{
					"message": "enabled 参数必须是布尔值",
				})
				return
			}

			cfg := h.routingManager.GetConfigCopy()
			cfg.Enabled = enabled

			if err := h.routingManager.Reload(cfg); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{
					"message": "切换失败: " + err.Error(),
				})
				return
			}

			msg := "策略路由已关闭"
			if enabled {
				msg = "策略路由已启用"
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message": msg,
				"enabled": enabled,
			})
			return
		}
	}

	// 否则按添加规则处理，从 raw map 构造 Rule
	rule := routing.Rule{Enabled: true}
	if v, ok := raw["name"].(string); ok {
		rule.Name = v
	}
	if v, ok := raw["wan"].(string); ok {
		rule.WAN = v
	}
	if v, ok := raw["type"].(string); ok {
		rule.Type = v
	}
	if ips, ok := raw["ips"].([]interface{}); ok {
		for _, ip := range ips {
			if s, ok := ip.(string); ok {
				rule.IPs = append(rule.IPs, s)
			}
		}
	}
	if apps, ok := raw["apps"].([]interface{}); ok {
		for _, app := range apps {
			if s, ok := app.(string); ok {
				rule.Apps = append(rule.Apps, s)
			}
		}
	}
	if v, ok := raw["game"].(string); ok {
		rule.Game = v
	}

	if rule.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "规则名称不能为空",
		})
		return
	}

	// 游戏规则：必须指定 .rules 文件且目录中存在，否则建出空 ipset 无意义
	if rule.Type == "game" {
		if rule.Game == "" {
			rule.Game = rule.Name
		}
		if err := h.validateGameRule(rule.Game); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
			return
		}
	}

	cfg := h.routingManager.GetConfigCopy()
	cfg.Rules = append(cfg.Rules, rule)

	if err := h.routingManager.Reload(cfg); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "添加规则失败: " + err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "规则添加成功",
		"rule":    rule,
	})
}

// handlePutRouting 更新路由规则
func (h *APIHandler) handlePutRouting(w http.ResponseWriter, r *http.Request) {
	if h.routingManager == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "路由管理器未初始化",
		})
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "规则名称不能为空",
		})
		return
	}

	var updatedRule routing.Rule
	if err := json.NewDecoder(r.Body).Decode(&updatedRule); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "请求参数格式错误",
		})
		return
	}

	// 游戏规则：校验 .rules 文件存在
	if updatedRule.Type == "game" {
		if updatedRule.Game == "" {
			updatedRule.Game = updatedRule.Name
		}
		if err := h.validateGameRule(updatedRule.Game); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
			return
		}
	}

	cfg := h.routingManager.GetConfigCopy()
	found := false
	for i, rule := range cfg.Rules {
		if rule.Name == name {
			cfg.Rules[i] = updatedRule
			found = true
			break
		}
	}

	if !found {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "规则不存在",
		})
		return
	}

	if err := h.routingManager.Reload(cfg); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "更新规则失败: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "规则更新成功",
		"rule":    updatedRule,
	})
}

// handleDeleteRouting 删除路由规则
func (h *APIHandler) handleDeleteRouting(w http.ResponseWriter, r *http.Request) {
	if h.routingManager == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "路由管理器未初始化",
		})
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "规则名称不能为空",
		})
		return
	}

	cfg := h.routingManager.GetConfigCopy()
	found := false
	for i, rule := range cfg.Rules {
		if rule.Name == name {
			cfg.Rules = append(cfg.Rules[:i], cfg.Rules[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "规则不存在",
		})
		return
	}

	if err := h.routingManager.Reload(cfg); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "删除规则失败: " + err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"message": "规则删除成功",
	})
}

// validateGameRule 校验游戏规则引用的 .rules 文件是否存在于游戏目录。
func (h *APIHandler) validateGameRule(gameKey string) error {
	dir := h.routingManager.GameRulesDir()
	if dir == "" {
		return fmt.Errorf("未配置游戏规则目录（game_rules_dir）")
	}
	if _, err := os.Stat(filepath.Join(dir, gameKey+".rules")); err != nil {
		return fmt.Errorf("未找到 %s.rules，请将规则文件放入 %s 目录后重试", gameKey, dir)
	}
	return nil
}

// handleGameLibrary 列出游戏规则目录下所有可用的 .rules 文件（只读）。
// 游戏的分配/启用走统一的 /api/v1/routing（Rule.Type="game"）。
// 该接口仅用于规则弹窗中的下拉选择器。
func (h *APIHandler) handleGameLibrary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if h.routingManager == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"rules_dir": "", "games": []interface{}{}})
		return
	}

	dir := h.routingManager.GameRulesDir()
	lib := []map[string]interface{}{}
	if dir != "" {
		if files, err := rules.ParseDir(dir); err == nil {
			keys := make([]string, 0, len(files))
			for k := range files {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				rf := files[k]
				lib = append(lib, map[string]interface{}{
					"name":       k,
					"title":      rf.Title,
					"subtitle":   rf.Subtitle,
					"source":     rf.Source,
					"cidr_count": len(rf.CIDRs),
					"warnings":   rf.Warnings,
				})
			}
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"rules_dir": dir,
		"games":     lib,
	})
}

// handleAppCatalog 获取应用目录
func (h *APIHandler) handleAppCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	categories := make(map[string][]dpi.ApplicationInfo)
	for _, app := range dpi.AppCatalog {
		categories[app.Category] = append(categories[app.Category], app)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"apps":       dpi.AppCatalog,
		"categories": categories,
	})
}

// handleDPIFlows 获取 DPI 识别的流列表
func (h *APIHandler) handleDPIFlows(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if h.routingManager == nil || h.routingManager.GetDPIDetector() == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled": false,
			"flows":   []dpi.FlowInfo{},
		})
		return
	}

	detector := h.routingManager.GetDPIDetector()
	flows := detector.GetAllFlows()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"enabled": true,
		"total":   len(flows),
		"flows":   flows,
	})
}

// handleISPLogo 返回运营商 SVG logo
func (h *APIHandler) handleISPLogo(w http.ResponseWriter, r *http.Request) {
	isp := r.URL.Query().Get("isp")
	if isp == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "缺少 isp 参数",
		})
		return
	}

	svg := web.GetISPSVG(isp)
	if svg == "" {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "未找到该运营商的 logo",
		})
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write([]byte(svg))
}

// handleEvents SSE 实时事件推送
func (h *APIHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE 不支持", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			data := map[string]interface{}{
				"type":    "update",
				"wans":    h.wanCollector.GetStats(),
				"clients": h.clientCollector.GetClients(),
				"time":    time.Now().Unix(),
			}
			jsonData, err := json.Marshal(data)
			if err != nil {
				log.Printf("SSE 序列化失败: %v", err)
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()
		}
	}
}
