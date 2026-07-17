package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Wooo0/wan-manager/internal/collector"
	"github.com/Wooo0/wan-manager/internal/routing"
	"github.com/Wooo0/wan-manager/internal/web"
)

// APIHandler API 处理器
type APIHandler struct {
	wanCollector    *collector.WANCollector
	clientCollector *collector.ClientCollector
	routingManager  *routing.Manager
}

// SummaryResponse 汇总响应
type SummaryResponse struct {
	WANS     []collector.WANStats    `json:"wans"`
	Clients  []collector.ClientInfo  `json:"clients"`
	Routing  *RoutingStatus          `json:"routing,omitempty"`
	UpdateAt time.Time               `json:"update_at"`
}

// RoutingStatus 路由状态
type RoutingStatus struct {
	Enabled bool             `json:"enabled"`
	Active  bool             `json:"active"`
	Config  *routing.RoutingConfig `json:"config,omitempty"`
}

// NewAPIHandler 创建 API 处理器
func NewAPIHandler(wc *collector.WANCollector, cc *collector.ClientCollector, rm *routing.Manager) *APIHandler {
	return &APIHandler{
		wanCollector:    wc,
		clientCollector: cc,
		routingManager:  rm,
	}
}

// RegisterRoutes 注册路由
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	// Web 界面
	mux.HandleFunc("/", h.handleRoot)
	
	// API 接口
	mux.HandleFunc("/api/v1/summary", h.handleSummary)
	mux.HandleFunc("/api/v1/wan", h.handleWAN)
	mux.HandleFunc("/api/v1/clients", h.handleClients)
	mux.HandleFunc("/api/v1/health", h.handleHealth)
	mux.HandleFunc("/api/v1/routing", h.handleRouting)
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
			"version": "running",
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

	if h.routingManager == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled": false,
			"active":  false,
			"message": "路由管理器未初始化",
		})
		return
	}

	switch r.Method {
	case "GET":
		// 获取路由配置和状态
		resp := map[string]interface{}{
			"enabled": h.routingManager.GetConfig().Enabled,
			"active":  h.routingManager.IsActive(),
			"config":  h.routingManager.GetConfig(),
		}
		json.NewEncoder(w).Encode(resp)

	case "POST":
		// 更新路由配置（TODO: 实现配置更新）
		w.WriteHeader(http.StatusNotImplemented)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "配置更新功能待实现",
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
