package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Wooo0/wan-manager/internal/collector"
)

// APIHandler API 处理器
type APIHandler struct {
	wanCollector    *collector.WANCollector
	clientCollector *collector.ClientCollector
}

// SummaryResponse 汇总响应
type SummaryResponse struct {
	WANS     []collector.WANStats    `json:"wans"`
	Clients  []collector.ClientInfo  `json:"clients"`
	UpdateAt time.Time              `json:"update_at"`
}

// NewAPIHandler 创建 API 处理器
func NewAPIHandler(wc *collector.WANCollector, cc *collector.ClientCollector) *APIHandler {
	return &APIHandler{
		wanCollector:    wc,
		clientCollector: cc,
	}
}

// RegisterRoutes 注册路由
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", h.handleRoot)
	mux.HandleFunc("/api/v1/summary", h.handleSummary)
	mux.HandleFunc("/api/v1/wan", h.handleWAN)
	mux.HandleFunc("/api/v1/clients", h.handleClients)
	mux.HandleFunc("/api/v1/health", h.handleHealth)
}

// handleRoot 根路径返回服务信息（避免浏览器访问 404）
func (h *APIHandler) handleRoot(w http.ResponseWriter, r *http.Request) {
	// 只处理精确匹配的根路径，其他未匹配路径返回 404
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":    "wan-manager",
		"version": "running",
		"endpoints": []string{
			"/api/v1/health",
			"/api/v1/summary",
			"/api/v1/wan",
			"/api/v1/clients",
		},
	})
}

func (h *APIHandler) handleSummary(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	resp := SummaryResponse{
		WANS:     h.wanCollector.GetStats(),
		Clients:  h.clientCollector.GetClients(),
		UpdateAt: time.Now(),
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
