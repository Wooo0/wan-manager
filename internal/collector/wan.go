package collector

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wooo0/wan-manager/internal/config"
)

// WANStats WAN 口统计数据
type WANStats struct {
	Name      string  `json:"name"`
	Label     string  `json:"label"`
	Interface string  `json:"interface"`
	RxBytes   uint64  `json:"rx_bytes"`
	TxBytes   uint64  `json:"tx_bytes"`
	RxSpeed   float64 `json:"rx_speed"`
	TxSpeed   float64 `json:"tx_speed"`
	Connected bool    `json:"connected"`
}

// WANCollector WAN 采集器
type WANCollector struct {
	wanConfigs []config.WANConfig
	interval   time.Duration
	mu         sync.RWMutex
	stats      []WANStats
	prevData   map[string]wanPrevData
}

type wanPrevData struct {
	rxBytes   uint64
	txBytes   uint64
	timestamp time.Time
}

// NewWANCollector 创建 WAN 采集器
func NewWANCollector(wans []config.WANConfig, interval int) *WANCollector {
	return &WANCollector{
		wanConfigs: wans,
		interval:   time.Duration(interval) * time.Second,
		prevData:   make(map[string]wanPrevData),
	}
}

// Start 启动采集循环
func (w *WANCollector) Start() {
	w.collect()
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for range ticker.C {
			w.collect()
		}
	}()
}

// GetStats 获取当前统计数据
func (w *WANCollector) GetStats() []WANStats {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]WANStats, len(w.stats))
	copy(result, w.stats)
	return result
}

func (w *WANCollector) collect() {
	var stats []WANStats
	now := time.Now()

	for _, wc := range w.wanConfigs {
		s := WANStats{
			Name:      wc.Name,
			Label:     wc.Label,
			Interface: wc.Interface,
		}

		rxPath := "/sys/class/net/" + wc.Interface + "/statistics/rx_bytes"
		txPath := "/sys/class/net/" + wc.Interface + "/statistics/tx_bytes"

		if rx, err := readUint64(rxPath); err == nil {
			s.RxBytes = rx
		}
		if tx, err := readUint64(txPath); err == nil {
			s.TxBytes = tx
		}

		prev, hasPrev := w.prevData[wc.Interface]
		if hasPrev && !prev.timestamp.IsZero() {
			elapsed := now.Sub(prev.timestamp).Seconds()
			if elapsed > 0 {
				if s.RxBytes >= prev.rxBytes {
					s.RxSpeed = float64(s.RxBytes-prev.rxBytes) / elapsed
				}
				if s.TxBytes >= prev.txBytes {
					s.TxSpeed = float64(s.TxBytes-prev.txBytes) / elapsed
				}
			}
		}

		s.Connected = s.RxBytes > 0 || s.TxBytes > 0
		stats = append(stats, s)

		w.prevData[wc.Interface] = wanPrevData{
			rxBytes:   s.RxBytes,
			txBytes:   s.TxBytes,
			timestamp: now,
		}
	}

	w.mu.Lock()
	w.stats = stats
	w.mu.Unlock()
}

func readUint64(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}
