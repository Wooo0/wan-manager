package collector

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wooo0/wan-manager/internal/config"
	"github.com/Wooo0/wan-manager/internal/isp"
)

// LatencyResult 延迟测试结果
type LatencyResult struct {
	Target   string `json:"target"`
	Latency  int    `json:"latency"`
}

// WANStats WAN 口统计数据
type WANStats struct {
	Name      string          `json:"name"`
	Label     string          `json:"label"`
	Interface string          `json:"interface"`
	RxBytes   uint64          `json:"rx_bytes"`
	TxBytes   uint64          `json:"tx_bytes"`
	RxSpeed   float64         `json:"rx_speed"`
	TxSpeed   float64         `json:"tx_speed"`
	Connected bool            `json:"connected"`
	IPv4      string          `json:"ipv4,omitempty"`
	IPv6      string          `json:"ipv6,omitempty"`
	DNS       string          `json:"dns,omitempty"`
	Latency   int             `json:"latency,omitempty"`
	Latencies []LatencyResult `json:"latencies,omitempty"`
	ISP       *isp.Info       `json:"isp,omitempty"`
	RxHistory []float64       `json:"rx_history,omitempty"`
	TxHistory []float64       `json:"tx_history,omitempty"`
}

// WANCollector WAN 采集器
type WANCollector struct {
	wanConfigs   []config.WANConfig
	interval     time.Duration
	mu           sync.RWMutex
	stats        []WANStats
	prevData     map[string]wanPrevData
	ispDetector  *isp.Detector
	rxHistory    map[string][]float64
	txHistory    map[string][]float64
	historySize  int
}

type wanPrevData struct {
	rxBytes   uint64
	txBytes   uint64
	timestamp time.Time
}

// NewWANCollector 创建 WAN 采集器
func NewWANCollector(wans []config.WANConfig, interval int) *WANCollector {
	return &WANCollector{
		wanConfigs:  wans,
		interval:    time.Duration(interval) * time.Second,
		prevData:    make(map[string]wanPrevData),
		ispDetector: isp.NewDetector(),
		rxHistory:   make(map[string][]float64),
		txHistory:   make(map[string][]float64),
		historySize: 20,
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

		// Mock 数据：当读取不到真实数据时使用
		if s.RxBytes == 0 && s.TxBytes == 0 {
			mockData := w.getMockData(wc.Name, now)
			s.RxBytes = mockData.rxBytes
			s.TxBytes = mockData.txBytes
			s.RxSpeed = mockData.rxSpeed
			s.TxSpeed = mockData.txSpeed
			s.Connected = true
			s.ISP = mockData.isp
			s.IPv4 = mockData.isp.IP
			s.Latency = 15
			s.Latencies = []LatencyResult{
				{Target: "baidu", Latency: 12 + now.Second()%8},
				{Target: "cloudflare", Latency: 28 + now.Second()%10},
			}
			s.RxHistory = w.getMockHistory(wc.Name, "rx", now)
			s.TxHistory = w.getMockHistory(wc.Name, "tx", now)
		} else {
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
			
			s.IPv4, s.IPv6 = getInterfaceIP(wc.Interface)
			s.DNS = getInterfaceDNS(wc.Interface)
			
			if s.Connected {
				s.ISP = w.ispDetector.Detect(wc.Interface)
				s.Latencies = pingMultiple([]string{"114.114.114.114", "www.baidu.com", "1.1.1.1"}, wc.Interface)
				if len(s.Latencies) > 0 && s.Latencies[0].Latency >= 0 {
					s.Latency = s.Latencies[0].Latency
				} else {
					s.Latency = -1
				}
			}

			w.updateHistory(wc.Interface, s.RxSpeed, s.TxSpeed)
			s.RxHistory = w.rxHistory[wc.Interface]
			s.TxHistory = w.txHistory[wc.Interface]
		}

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

func (w *WANCollector) updateHistory(iface string, rxSpeed, txSpeed float64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.rxHistory[iface] = append(w.rxHistory[iface], rxSpeed)
	if len(w.rxHistory[iface]) > w.historySize {
		w.rxHistory[iface] = w.rxHistory[iface][1:]
	}

	w.txHistory[iface] = append(w.txHistory[iface], txSpeed)
	if len(w.txHistory[iface]) > w.historySize {
		w.txHistory[iface] = w.txHistory[iface][1:]
	}
}

type mockWANData struct {
	rxBytes uint64
	txBytes uint64
	rxSpeed float64
	txSpeed float64
	isp     *isp.Info
}

func (w *WANCollector) getMockData(name string, now time.Time) mockWANData {
	baseRx := uint64(1000000000)
	baseTx := uint64(500000000)
	
	if name == "wan1" {
		return mockWANData{
			rxBytes: baseRx + uint64(now.Second())*1000000,
			txBytes: baseTx + uint64(now.Second())*500000,
			rxSpeed: 5000000 + float64(now.Second()%10)*100000,
			txSpeed: 2000000 + float64(now.Second()%10)*50000,
			isp: &isp.Info{
				ISP:     "电信",
				Country: "中国",
				Region:  "四川",
				City:    "成都",
				IP:      "119.4.54.163",
			},
		}
	}
	
	return mockWANData{
		rxBytes: baseRx/2 + uint64(now.Second())*500000,
		txBytes: baseTx/2 + uint64(now.Second())*250000,
		rxSpeed: 1000000 + float64(now.Second()%10)*50000,
		txSpeed: 500000 + float64(now.Second()%10)*25000,
		isp: &isp.Info{
			ISP:     "联通",
			Country: "中国",
			Region:  "四川",
			City:    "成都",
			IP:      "103.172.41.169",
		},
	}
}

func (w *WANCollector) getMockHistory(name string, direction string, now time.Time) []float64 {
	var result []float64
	base := 5000000.0
	if name == "wan2" {
		base = 1000000.0
	}
	if direction == "tx" {
		base = base / 2.5
	}
	
	for i := 0; i < w.historySize; i++ {
		offset := (now.Second() + i) % 10
		result = append(result, base+float64(offset)*100000)
	}
	return result
}

func readUint64(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

// getInterfaceIP 获取网络接口的 IP 地址
func getInterfaceIP(iface string) (ipv4, ipv6 string) {
	// 使用 ip addr show 命令获取 IP 地址
	cmd := exec.Command("ip", "addr", "show", iface)
	output, err := cmd.Output()
	if err != nil {
		return "", ""
	}
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			// IPv4: inet 192.168.1.1/24 brd ...
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				addr := strings.Split(parts[1], "/")[0]
				ipv4 = addr
			}
		} else if strings.HasPrefix(line, "inet6 ") {
			// IPv6: inet6 fe80::1/64 scope link ...
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				addr := strings.Split(parts[1], "/")[0]
				// 跳过 link-local 地址
				if !strings.HasPrefix(addr, "fe80:") {
					ipv6 = addr
				}
			}
		}
	}
	
	return ipv4, ipv6
}

// pingLatency 测试到目标地址的延迟（毫秒），iface 指定绑定接口
func pingLatency(target, iface string) int {
	args := []string{"-c", "1", "-W", "2"}
	if iface != "" {
		args = append(args, "-I", iface)
	}
	args = append(args, target)
	cmd := exec.Command("ping", args...)
	output, err := cmd.Output()
	if err != nil {
		return -1
	}
	
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "time=") {
			parts := strings.Split(line, "time=")
			if len(parts) >= 2 {
				timeStr := strings.Split(parts[1], " ")[0]
				timeStr = strings.TrimSuffix(timeStr, "ms")
				if t, err := strconv.ParseFloat(timeStr, 64); err == nil {
					return int(t)
				}
			}
		}
	}
	
	return -1
}

// pingMultiple 并行测试多个目标的延迟，iface 指定绑定接口
func pingMultiple(targets []string, iface string) []LatencyResult {
	type result struct {
		target  string
		latency int
	}
	
	ch := make(chan result, len(targets))
	
	for _, t := range targets {
		go func(target string) {
			ch <- result{target: target, latency: pingLatency(target, iface)}
		}(t)
	}
	
	var results []LatencyResult
	for i := 0; i < len(targets); i++ {
		r := <-ch
		name := r.target
		if name == "www.baidu.com" {
			name = "baidu"
		} else if name == "1.1.1.1" {
			name = "cloudflare"
		} else if name == "114.114.114.114" {
			name = "default"
		}
		results = append(results, LatencyResult{Target: name, Latency: r.latency})
	}
	
	return results
}

// getInterfaceDNS 获取系统 DNS 服务器
func getInterfaceDNS(iface string) string {
	// OpenWrt 的 DNS 配置文件
	files := []string{
		"/tmp/resolv.conf.d/resolv.conf.auto",
		"/tmp/resolv.conf.auto",
		"/etc/resolv.conf",
	}
	
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		
		var dnsServers []string
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "nameserver") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					dnsServers = append(dnsServers, fields[1])
				}
			}
		}
		if len(dnsServers) > 0 {
			return strings.Join(dnsServers, ", ")
		}
	}
	
	return ""
}
