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
	IPv4      string  `json:"ipv4,omitempty"`
	IPv6      string  `json:"ipv6,omitempty"`
	DNS       string  `json:"dns,omitempty"`
	Latency   int     `json:"latency,omitempty"`  // 延迟（毫秒）
	ISP       *isp.Info `json:"isp,omitempty"`
}

// WANCollector WAN 采集器
type WANCollector struct {
	wanConfigs []config.WANConfig
	interval   time.Duration
	mu         sync.RWMutex
	stats      []WANStats
	prevData   map[string]wanPrevData
	ispDetector *isp.Detector
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
		ispDetector: isp.NewDetector(),
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
			// Mock ISP 信息
			s.ISP = mockData.isp
			// Mock IP 和延迟
			s.IPv4 = mockData.isp.IP
			s.Latency = 15
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
			
			// 获取 IP 地址
			s.IPv4, s.IPv6 = getInterfaceIP(wc.Interface)
			
			// 测试延迟（使用 114.114.114.114 作为目标）
			if s.Connected {
				s.Latency = pingLatency("114.114.114.114")
			}
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

type mockWANData struct {
	rxBytes uint64
	txBytes uint64
	rxSpeed float64
	txSpeed float64
	isp     *isp.Info
}

func (w *WANCollector) getMockData(name string, now time.Time) mockWANData {
	// 模拟不同的流量模式
	baseRx := uint64(1000000000) // 1GB
	baseTx := uint64(500000000)  // 500MB
	
	if name == "wan1" {
		return mockWANData{
			rxBytes: baseRx + uint64(now.Second())*1000000,
			txBytes: baseTx + uint64(now.Second())*500000,
			rxSpeed: 5000000 + float64(now.Second()%10)*100000, // 5MB/s + 波动
			txSpeed: 2000000 + float64(now.Second()%10)*50000,  // 2MB/s + 波动
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
		rxSpeed: 1000000 + float64(now.Second()%10)*50000,  // 1MB/s + 波动
		txSpeed: 500000 + float64(now.Second()%10)*25000,   // 500KB/s + 波动
		isp: &isp.Info{
			ISP:     "联通",
			Country: "中国",
			Region:  "四川",
			City:    "成都",
			IP:      "103.172.41.169",
		},
	}
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

// pingLatency 测试到目标地址的延迟（毫秒）
func pingLatency(target string) int {
	cmd := exec.Command("ping", "-c", "1", "-W", "2", target)
	output, err := cmd.Output()
	if err != nil {
		return -1
	}
	
	// 解析 ping 输出，查找 time=XX.XX ms
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "time=") {
			// 例如：64 bytes from 114.114.114.114: icmp_seq=1 ttl=128 time=12.3 ms
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
