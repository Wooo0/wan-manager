package collector

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wooo0/wan-manager/internal/config"
	"github.com/Wooo0/wan-manager/internal/isp"
)

// LatencyResult 延迟测试结果
type LatencyResult struct {
	Target    string `json:"target"`
	Latency   int    `json:"latency"`
	Latencies []int  `json:"latencies"`
}

// WANStats WAN 口统计数据
type WANStats struct {
	Name      string          `json:"name"`
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
	wanConfigs  []config.WANConfig
	interval    time.Duration
	mu          sync.RWMutex
	stats       []WANStats
	prevData    map[string]wanPrevData
	ispDetector *isp.Detector
	rxHistory   map[string][]float64
	txHistory   map[string][]float64
	historySize int
	collecting  atomic.Bool
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
		historySize: 40,
	}
}

// Start 启动采集循环
func (w *WANCollector) Start() {
	w.collect()
	go func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		for range ticker.C {
			// 防止上一次采集（含 ping 等阻塞调用）尚未完成时叠加触发，
			// 避免在低采集间隔下出现 ping 进程堆积。
			if !w.collecting.CompareAndSwap(false, true) {
				continue
			}
			w.collect()
			w.collecting.Store(false)
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
				{Target: "baidu", Latency: 12 + now.Second()%8, Latencies: mockLatencyHistory(12, now)},
				{Target: "cloudflare", Latency: 28 + now.Second()%10, Latencies: mockLatencyHistory(28, now)},
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
				s.Latencies = w.pingWithHistory([]string{"114.114.114.114", "www.baidu.com", "1.1.1.1"}, wc.Interface)
				if len(s.Latencies) > 0 && s.Latencies[0].Latency >= 0 {
					s.Latency = s.Latencies[0].Latency
				} else {
					s.Latency = -1
				}
			}

			w.updateHistory(wc.Interface, s.RxSpeed, s.TxSpeed)
			// 复制历史切片（而非共享底层数组），避免与 GetStats 返回的
			// 切片在后续 append 时并发读写同一底层数组导致 data race。
			s.RxHistory, s.TxHistory = w.snapshotHistory(wc.Interface)
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

// snapshotHistory 在锁内复制指定接口的 rx/tx 历史切片，返回独立数组，
// 供 GetStats 对外返回，避免与 updateHistory 的 append 共享底层数组引发 data race。
func (w *WANCollector) snapshotHistory(iface string) (rx, tx []float64) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	rx = append([]float64(nil), w.rxHistory[iface]...)
	tx = append([]float64(nil), w.txHistory[iface]...)
	return
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

// mockLatencyHistory 生成一组带波动的延迟历史值（模拟连续 ping 的抖动），
// 让前端延迟柱状图呈现真实起伏，而非全部等高。base 为该目标基准延迟（ms）。
func mockLatencyHistory(base int, now time.Time) []int {
	// 每秒整体漂移，使每次刷新都有变化（"循环"感）；每个采样点再叠加固定形状抖动。
	drift := now.Second() % 9
	shape := []int{-3, 2, 5, -1, 3} // 5 个采样点的相对起伏
	result := make([]int, len(shape))
	for i, d := range shape {
		v := base + drift + d
		if v < 1 {
			v = 1
		}
		result[i] = v
	}
	return result
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
// count 指定 ping 的次数，返回每次的延迟数组
func pingLatency(target, iface string, count int) []int {
	args := []string{"-c", strconv.Itoa(count), "-W", "2"}
	if iface != "" {
		args = append(args, "-I", iface)
	}
	args = append(args, target)
	cmd := exec.Command("ping", args...)
	output, err := cmd.Output()
	if err != nil {
		return make([]int, count)
	}

	// 解析每次 ping 的 time=
	lines := strings.Split(string(output), "\n")
	var latencies []int
	for _, line := range lines {
		if strings.Contains(line, "time=") {
			parts := strings.Split(line, "time=")
			if len(parts) >= 2 {
				timeStr := strings.Split(parts[1], " ")[0]
				timeStr = strings.TrimSuffix(timeStr, "ms")
				if t, err := strconv.ParseFloat(timeStr, 64); err == nil {
					latencies = append(latencies, int(t))
				}
			}
		}
	}

	// 如果解析到的次数不够，用 -1 填充
	for len(latencies) < count {
		latencies = append(latencies, -1)
	}

	return latencies[:count]
}

// pingWithHistory 并行测试多个目标的延迟，每个目标 ping 5 次
func (w *WANCollector) pingWithHistory(targets []string, iface string) []LatencyResult {
	type result struct {
		target    string
		latencies []int
	}

	ch := make(chan result, len(targets))

	for _, t := range targets {
		go func(target string) {
			// 每个目标 ping 5 次，返回 5 个延迟值
			ch <- result{target: target, latencies: pingLatency(target, iface, 5)}
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

		// 计算平均延迟用于显示
		// 注意：avgLatency 默认 -1 表示「无有效数据」哨兵值；
		// 求和用独立的 sum，避免把哨兵值 -1 也算进平均值（否则会偏小 1）。
		var avgLatency int = -1
		var sum int
		var validCount int
		for _, lat := range r.latencies {
			if lat >= 0 {
				sum += lat
				validCount++
			}
		}
		if validCount > 0 {
			avgLatency = sum / validCount
		}

		results = append(results, LatencyResult{
			Target:    name,
			Latency:   avgLatency,
			Latencies: r.latencies, // 5 个独立延迟值
		})
	}

	return results
}

// getInterfaceDNS 获取指定接口的 DNS 服务器。
// OpenWrt 下优先读取按接口隔离的 resolv 文件（/tmp/resolv.conf.d/<iface>.resolv.conf），
// 若不存在则回退到全局 resolv.conf.* 文件。
func getInterfaceDNS(iface string) string {
	files := []string{
		"/tmp/resolv.conf.d/" + iface + ".resolv.conf",
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
