package collector

import (
	"bufio"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ClientInfo 客户端信息
type ClientInfo struct {
	MAC       string `json:"mac"`
	IP        string `json:"ip"`
	Name      string `json:"name"`
	SSID      string `json:"ssid"`
	Band      string `json:"band"`
	Signal    int    `json:"signal"`
	ConnType  string `json:"conn_type"`
	Online    bool   `json:"online"`
	Interface string `json:"interface"`
}

// ClientCollector 客户端采集器
type ClientCollector struct {
	interval time.Duration
	mu       sync.RWMutex
	clients  []ClientInfo
}

// NewClientCollector 创建客户端采集器
func NewClientCollector(interval int) *ClientCollector {
	return &ClientCollector{
		interval: time.Duration(interval) * time.Second,
	}
}

// Start 启动采集循环
func (c *ClientCollector) Start() {
	c.collect()
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for range ticker.C {
			c.collect()
		}
	}()
}

// GetClients 获取当前客户端列表
func (c *ClientCollector) GetClients() []ClientInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]ClientInfo, len(c.clients))
	copy(result, c.clients)
	return result
}

func (c *ClientCollector) collect() {
	wifiClients := collectWifiClients()
	arpMap := collectARPMap()
	dhcpMap := collectDHCPMap()

	// 如果没有采集到真实数据，使用 mock 数据
	if len(wifiClients) == 0 && len(arpMap) == 0 {
		log.Printf("使用 mock 客户端数据")
		c.mu.Lock()
		c.clients = c.getMockClients()
		c.mu.Unlock()
		return
	}

	// 调试日志：打印采集到的数据量
	log.Printf("采集到 %d 个 WiFi 客户端，ARP 表 %d 条，DHCP 租约 %d 条",
		len(wifiClients), len(arpMap), len(dhcpMap))

	seen := make(map[string]bool)
	var result []ClientInfo

	for _, wc := range wifiClients {
		mac := normalizeMAC(wc.MAC)
		if seen[mac] {
			continue
		}
		seen[mac] = true

		// 优先从 ARP 表获取 IP，否则从 DHCP 租约获取
		if arp, ok := arpMap[mac]; ok {
			wc.IP = arp.IP
			log.Printf("WiFi 客户端 %s 从 ARP 表获取 IP: %s", mac, arp.IP)
		} else if dhcp, ok := dhcpMap[mac]; ok {
			wc.IP = dhcp.IP
			log.Printf("WiFi 客户端 %s 从 DHCP 租约获取 IP: %s", mac, dhcp.IP)
		} else {
			log.Printf("WiFi 客户端 %s 未找到 IP 地址", mac)
		}

		// 从 DHCP 租约获取主机名
		if dhcp, ok := dhcpMap[mac]; ok && dhcp.Name != "" {
			wc.Name = dhcp.Name
		}
		if wc.Name == "" {
			wc.Name = wc.IP
		}
		if wc.Name == "" {
			wc.Name = mac
		}

		result = append(result, wc)
	}

	for mac, arp := range arpMap {
		if seen[mac] {
			continue
		}
		seen[mac] = true

		client := ClientInfo{
			MAC:       mac,
			IP:        arp.IP,
			ConnType:  "wired",
			Online:    true,
			Interface: arp.Device,
		}
		if dhcp, ok := dhcpMap[mac]; ok && dhcp.Name != "" {
			client.Name = dhcp.Name
		}
		if client.Name == "" {
			client.Name = client.IP
		}

		result = append(result, client)
	}

	c.mu.Lock()
	c.clients = result
	c.mu.Unlock()
}

// getMockClients 返回 mock 客户端数据
func (c *ClientCollector) getMockClients() []ClientInfo {
	return []ClientInfo{
		{MAC: "9c:b8:b4:c6:bb:96", IP: "192.168.1.70", Name: "Midea_9C?B8?B4?C6?BB?96", SSID: "Wang.Tao_5G_Game", Band: "5G", Signal: -55, ConnType: "wifi", Online: true, Interface: "wl0"},
		{MAC: "b0:6b:11:a6:3f:3c", IP: "192.168.1.182", Name: "98Q10L-TV", SSID: "Wang.Tao_5G_Game", Band: "5G", Signal: -40, ConnType: "wifi", Online: true, Interface: "wl0"},
		{MAC: "50:88:11:f4:b3:b5", IP: "192.168.1.178", Name: "508811f4b3b5", SSID: "Wang.Tao_5G_Game", Band: "5G", Signal: -58, ConnType: "wifi", Online: true, Interface: "wl0"},
		{MAC: "54:ef:44:7e:f5:07", IP: "192.168.1.156", Name: "54:ef:44:7e:f5:07", SSID: "Wang.Tao_5G_Game", Band: "5G", Signal: -63, ConnType: "wifi", Online: true, Interface: "wl0"},
		{MAC: "e8:f6:0a:fc:b2:6c", IP: "192.168.1.95", Name: "esp32c6-fan-controller", SSID: "Wang.Tao", Band: "2.4G", Signal: -42, ConnType: "wifi", Online: true, Interface: "wl1"},
		{MAC: "d8:d2:61:9d:cd:0e", IP: "192.168.1.88", Name: "midea_ac_0202", SSID: "Wang.Tao", Band: "2.4G", Signal: -64, ConnType: "wifi", Online: true, Interface: "wl1"},
		{MAC: "ec:4d:3e:8a:22:b9", IP: "192.168.1.102", Name: "ec4d3e8a22b9", SSID: "Wang.Tao", Band: "2.4G", Signal: -9, ConnType: "wifi", Online: true, Interface: "wl1"},
		{MAC: "70:c9:32:c1:f5:80", IP: "192.168.1.115", Name: "dreame_vacuum_r9506", SSID: "Wang.Tao", Band: "2.4G", Signal: -61, ConnType: "wifi", Online: true, Interface: "wl1"},
	}
}

type arpEntry struct {
	IP     string
	Device string
}

var macRegex = regexp.MustCompile(`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`)

func normalizeMAC(mac string) string {
	return strings.ToLower(mac)
}

func isValidMAC(mac string) bool {
	return macRegex.MatchString(mac)
}

func getWifiInterfaces() []string {
	var ifaces []string

	if out, err := exec.Command("iwinfo").Output(); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) > 0 {
				name := fields[0]
				if strings.HasPrefix(name, "wl") || strings.HasPrefix(name, "ra") || strings.HasPrefix(name, "wlan") {
					ifaces = append(ifaces, name)
				}
			}
		}
	}

	if len(ifaces) == 0 {
		if out, err := exec.Command("iw", "dev").Output(); err == nil {
			scanner := bufio.NewScanner(strings.NewReader(string(out)))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if strings.HasPrefix(line, "Interface ") {
					name := strings.TrimSpace(strings.TrimPrefix(line, "Interface "))
					if name != "" {
						ifaces = append(ifaces, name)
					}
				}
			}
		}
	}

	if len(ifaces) == 0 {
		if entries, err := os.ReadDir("/sys/class/net/"); err == nil {
			for _, e := range entries {
				name := e.Name()
				if strings.HasPrefix(name, "wl") || strings.HasPrefix(name, "ra") || strings.HasPrefix(name, "wlan") {
					ifaces = append(ifaces, name)
				}
			}
		}
	}

	return ifaces
}

func getSSID(iface string) string {
	if out, err := exec.Command("iwinfo", iface, "info").Output(); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "ESSID:") || strings.Contains(line, "SSID:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					ssid := strings.TrimSpace(parts[1])
					ssid = strings.Trim(ssid, `"`)
					if ssid != "" && ssid != "unknown" {
						return ssid
					}
				}
			}
		}
	}

	if out, err := exec.Command("iw", "dev", iface, "info").Output(); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "ssid ") {
				return strings.TrimSpace(strings.TrimPrefix(line, "ssid "))
			}
		}
	}

	return ""
}

func getBand(iface string) string {
	switch {
	case strings.HasSuffix(iface, "0"), strings.HasSuffix(iface, "2"),
		strings.HasSuffix(iface, "5"), strings.HasSuffix(iface, "7"),
		strings.HasSuffix(iface, "12"), strings.HasSuffix(iface, "14"),
		strings.HasSuffix(iface, "20"), strings.HasSuffix(iface, "24"):
		return "5G"
	case strings.HasSuffix(iface, "1"), strings.HasSuffix(iface, "3"),
		strings.HasSuffix(iface, "6"), strings.HasSuffix(iface, "13"),
		strings.HasSuffix(iface, "15"), strings.HasSuffix(iface, "21"),
		strings.HasSuffix(iface, "25"):
		return "2.4G"
	}

	if out, err := exec.Command("iwinfo", iface, "info").Output(); err == nil {
		lower := strings.ToLower(string(out))
		if strings.Contains(lower, "5 ghz") || strings.Contains(lower, "5ghz") {
			return "5G"
		}
		if strings.Contains(lower, "2.4 ghz") || strings.Contains(lower, "2.4ghz") {
			return "2.4G"
		}
	}

	return ""
}

func collectWifiClients() []ClientInfo {
	var clients []ClientInfo
	ifaces := getWifiInterfaces()

	for _, iface := range ifaces {
		ssid := getSSID(iface)
		band := getBand(iface)

		assoc := getIwinfoAssoclist(iface)
		if len(assoc) == 0 {
			assoc = getIwStationDump(iface)
		}

		for mac, signal := range assoc {
			clients = append(clients, ClientInfo{
				MAC:       normalizeMAC(mac),
				SSID:      ssid,
				Band:      band,
				Signal:    signal,
				ConnType:  "wifi",
				Online:    true,
				Interface: iface,
			})
		}
	}

	return clients
}

func getIwinfoAssoclist(iface string) map[string]int {
	result := make(map[string]int)

	out, err := exec.Command("iwinfo", iface, "assoclist").Output()
	if err != nil {
		return result
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 跳过表头、分隔线和元数据行
		if line == "" || strings.Contains(line, "MAC") || strings.Contains(line, "---") ||
			strings.Contains(line, "ifname") || strings.Contains(line, "ssid:") ||
			strings.Contains(line, "bssid") || strings.Contains(line, "channel") ||
			strings.Contains(line, "noise") || strings.Contains(line, "stacount") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		mac := fields[0]
		if !isValidMAC(mac) {
			continue
		}

		// iwinfo assoclist 输出格式：MAC PHY SECU RSSI NOISE SNR ...
		// fields[3] 是 RSSI（信号强度，如 "-55"）
		signal := 0
		if n, err := parseInt(fields[3]); err == nil {
			signal = n
		}

		result[mac] = signal
	}

	return result
}

func getIwStationDump(iface string) map[string]int {
	result := make(map[string]int)

	out, err := exec.Command("iw", "dev", iface, "station", "dump").Output()
	if err != nil {
		return result
	}

	var currentMAC string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Station ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				currentMAC = parts[1]
				if isValidMAC(currentMAC) {
					result[currentMAC] = 0
				}
			}
			continue
		}

		if currentMAC != "" && strings.Contains(line, "signal:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				valStr := strings.TrimSpace(parts[1])
				valFields := strings.Fields(valStr)
				if len(valFields) > 0 {
					if n, err := parseInt(valFields[0]); err == nil {
						result[currentMAC] = n
					}
				}
			}
		}
	}

	return result
}

func collectARPMap() map[string]arpEntry {
	result := make(map[string]arpEntry)

	data, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		log.Printf("读取 /proc/net/arp 失败: %v", err)
		return result
	}

	// 调试日志：打印原始文件内容
	log.Printf("ARP 文件原始内容 (%d 字节): %s", len(data), string(data))

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum == 1 {
			continue
		}
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 6 {
			log.Printf("ARP 行字段不足 6 个: %q (字段数: %d)", line, len(fields))
			continue
		}

		ip := fields[0]
		hwType := fields[2]
		mac := fields[3]
		device := fields[5]

		if hwType != "0x1" {
			log.Printf("ARP 跳过非以太网类型: %s (hwType: %s)", mac, hwType)
			continue
		}
		if !isValidMAC(mac) {
			log.Printf("ARP 跳过无效 MAC: %s", mac)
			continue
		}
		if mac == "00:00:00:00:00:00" {
			continue
		}

		result[normalizeMAC(mac)] = arpEntry{
			IP:     ip,
			Device: device,
		}
	}

	log.Printf("ARP 表最终解析到 %d 条记录", len(result))
	return result
}

type dhcpEntry struct {
	IP   string
	Name string
}

func collectDHCPMap() map[string]dhcpEntry {
	result := make(map[string]dhcpEntry)

	data, err := os.ReadFile("/tmp/dhcp.leases")
	if err != nil {
		log.Printf("读取 /tmp/dhcp.leases 失败: %v", err)
		return result
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 {
			mac := normalizeMAC(fields[1])
			ip := fields[2]
			name := fields[3]
			entry := dhcpEntry{IP: ip}
			if name != "*" && name != "" {
				entry.Name = name
			}
			result[mac] = entry
		}
	}

	log.Printf("DHCP 租约文件解析到 %d 条记录", len(result))
	return result
}

func parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	negative := false
	if strings.HasPrefix(s, "-") {
		negative = true
		s = s[1:]
	}

	var result int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		} else {
			break
		}
	}

	if negative {
		result = -result
	}

	return result, nil
}
