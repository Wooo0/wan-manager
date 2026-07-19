package collector

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
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
	Band      string `json:"band"`       // "2.4G" / "5.2G" / "5.8G"
	Channel   int    `json:"channel"`    // 实际信道号
	Signal    int    `json:"signal"`
	ConnType  string `json:"conn_type"`  // "wifi" / "wired"
	Online    bool   `json:"online"`
	Interface string `json:"interface"`
	Node      string `json:"node"`       // 所属 mesh 节点名
	NodeMAC   string `json:"node_mac"`   // 所属 mesh 节点 MAC
	ParentMAC string `json:"parent_mac"` // 父节点 MAC
	IsMeshAP  bool   `json:"is_mesh_ap"` // 是否 mesh AP 节点
}

// ClientCollector 客户端采集器
type ClientCollector struct {
	interval     time.Duration
	mu           sync.RWMutex
	clients      []ClientInfo
	miwifi       *MiWiFiClient
	nodeNames    map[string]string // MAC → 节点名称 缓存
}

// NewClientCollector 创建客户端采集器
// miwifiPassword: 小米路由器 admin 密码，为空则不启用 MiWiFi API
func NewClientCollector(interval int, miwifiPassword string) *ClientCollector {
	return &ClientCollector{
		interval:  time.Duration(interval) * time.Second,
		miwifi:    NewMiWiFiClient(miwifiPassword),
		nodeNames: make(map[string]string),
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
	// ===== 数据源 =====
	arpMap := collectARPMap()
	dhcpMap := collectDHCPMap()

	// 尝试通过 MiWiFi API 获取全量设备（含 mesh 子节点上的设备）
	var miDevices []MiWiFiDevice
	var miWiFiSignals map[string]int
	var miNewStatus *MiWiFiNewStatus

	if c.miwifi.IsAvailable() {
		var err error
		miDevices, err = c.miwifi.GetDeviceList()
		if err != nil {
			log.Printf("MiWiFi devicelist 获取失败: %v，回退到 iwinfo", err)
		} else {
			log.Printf("MiWiFi devicelist: %d 台设备", len(miDevices))
		}

		wifiDevs, err := c.miwifi.GetWiFiConnectDevices()
		if err == nil {
			miWiFiSignals = make(map[string]int, len(wifiDevs))
			for _, wd := range wifiDevs {
				miWiFiSignals[normalizeMAC(wd.MAC)] = wd.Signal
			}
		}

		miNewStatus, _ = c.miwifi.GetNewStatus()
	}

	// 通过 iwinfo 获取本地 WiFi 客户端（信号、信道、频段）
	wifiClients := collectWifiClients()
	// 构建 MAC → iwinfo 数据映射
	type wifiDetail struct {
		ssid  string
		band  string
		ch    int
		signal int
		iface string
	}
	wifiMap := make(map[string]wifiDetail, len(wifiClients))
	for _, wc := range wifiClients {
		mac := normalizeMAC(wc.MAC)
		wifiMap[mac] = wifiDetail{
			ssid:   wc.SSID,
			band:   wc.Band,
			signal: wc.Signal,
			iface:  wc.Interface,
			ch:     getChannelForInterface(wc.Interface),
		}
	}

	// 无数据时回退 mock
	if len(miDevices) == 0 && len(wifiClients) == 0 && len(arpMap) == 0 {
		log.Printf("使用 mock 客户端数据")
		c.mu.Lock()
		c.clients = c.getMockClients()
		c.mu.Unlock()
		return
	}

	// 收集所有 MAC → node 名称映射
	c.updateNodeNames(miDevices)

	// ===== 合并 =====
	seen := make(map[string]bool)
	var result []ClientInfo

	// 主路径: MiWiFi API 数据
	for _, md := range miDevices {
		mac := normalizeMAC(md.MAC)
		if seen[mac] {
			continue
		}
		seen[mac] = true

		client := ClientInfo{
			MAC:      mac,
			Online:   md.Online == 1,
			IsMeshAP: md.IsAP == 1,
			ParentMAC: normalizeMAC(md.Parent),
			Node:     c.nodeNames[normalizeMAC(md.Parent)],
		}

		// IP + 名称
		if len(md.IP) > 0 {
			client.IP = md.IP[0].IP
		}
		client.Name = md.Name
		if client.Name == "" {
			client.Name = md.OName
		}

		// 连接类型
		switch md.Type {
		case 0:
			client.ConnType = "wired"
		case 1:
			client.ConnType = "wifi"
		case 2:
			client.ConnType = "wifi"
		case 3:
			client.ConnType = "wifi"
		}

		// 用 iwinfo 数据补充信号、信道、精确频段
		if wd, ok := wifiMap[mac]; ok {
			client.Signal = wd.signal
			client.SSID = wd.ssid
			client.Interface = wd.iface
			client.Channel = wd.ch
			client.Band = bandFromChannel(wd.ch)
		}
		// 用 wifi_connect_devices 的信号补充
		if client.Signal == 0 && miWiFiSignals != nil {
			if sig, ok := miWiFiSignals[mac]; ok {
				// 转换：API 信号值是 0-255，需映射为负数 dBm
				client.Signal = signalFromMiWiFi(sig)
			}
		}
		// 频段兜底
		if client.Band == "" {
			client.Band = bandFromMiWiFiType(md.Type)
		}
		// SSID 兜底
		if client.SSID == "" {
			client.SSID = getSSIDFromStatus(miNewStatus, md.Type)
		}

		// 有线设备补 IP/名称
		if client.IP == "" {
			if arp, ok := arpMap[mac]; ok {
				client.IP = arp.IP
			} else if dhcp, ok := dhcpMap[mac]; ok {
				client.IP = dhcp.IP
			}
		}
		if client.Name == "" || client.Name == mac {
			if dhcp, ok := dhcpMap[mac]; ok && dhcp.Name != "" {
				client.Name = dhcp.Name
			}
		}
		if client.Name == "" {
			client.Name = client.IP
		}
		if client.Name == "" {
			client.Name = mac
		}

		result = append(result, client)
	}

	// 补充: iwinfo 中有但 MiWiFi 中缺失的
	for _, wc := range wifiClients {
		mac := normalizeMAC(wc.MAC)
		if seen[mac] {
			continue
		}
		seen[mac] = true

		client := ClientInfo{
			MAC:       mac,
			SSID:      wc.SSID,
			Band:      bandFromChannel(getChannelForInterface(wc.Interface)),
			Channel:   getChannelForInterface(wc.Interface),
			Signal:    wc.Signal,
			ConnType:  "wifi",
			Online:    true,
			Interface: wc.Interface,
		}
		if arp, ok := arpMap[mac]; ok {
			client.IP = arp.IP
		} else if dhcp, ok := dhcpMap[mac]; ok {
			client.IP = dhcp.IP
		}
		if dhcp, ok := dhcpMap[mac]; ok && dhcp.Name != "" {
			client.Name = dhcp.Name
		}
		if client.Name == "" {
			client.Name = client.IP
		}
		if client.Name == "" {
			client.Name = mac
		}

		result = append(result, client)
	}

	// 补充: ARP 中的有线设备（不在 WiFi 中）
	for mac, arp := range arpMap {
		if seen[mac] {
			continue
		}
		seen[mac] = true

		client := ClientInfo{
			MAC:      mac,
			IP:       arp.IP,
			ConnType: "wired",
			Online:   true,
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

// updateNodeNames 从设备列表中提取 mesh AP 节点的 MAC→名称 映射
func (c *ClientCollector) updateNodeNames(devices []MiWiFiDevice) {
	for _, d := range devices {
		if d.IsAP == 1 {
			mac := normalizeMAC(d.MAC)
			if _, exists := c.nodeNames[mac]; !exists {
				c.nodeNames[mac] = d.Name
				if d.Name == "" || d.Name == mac {
					c.nodeNames[mac] = "Mesh-" + mac[len(mac)-5:] // 简短别名
				}
			}
		}
	}
	// 确保主路由也有名字
	if _, exists := c.nodeNames[""]; !exists {
		c.nodeNames[""] = "主路由"
	}
}

// bandFromChannel 根据信道号返回精确频段名
func bandFromChannel(ch int) string {
	switch {
	case ch >= 1 && ch <= 14:
		return "2.4G"
	case ch >= 36 && ch <= 64:
		return "5.2G"
	case ch >= 149 && ch <= 165:
		return "5.8G"
	default:
		return ""
	}
}

// bandFromMiWiFiType 根据 MiWiFi type 字段返回粗略频段
func bandFromMiWiFiType(t int) string {
	switch t {
	case 1:
		return "2.4G"
	case 2:
		return "5G"
	default:
		return ""
	}
}

// getSSIDFromStatus 尝试从 newstatus 获取 SSID
func getSSIDFromStatus(status *MiWiFiNewStatus, devType int) string {
	if status == nil {
		return ""
	}
	switch devType {
	case 1: // 2.4G
		return status.Band24G.SSID
	case 2: // 5G Game
		if status.Band5G.SSID != "" {
			return status.Band5G.SSID
		}
		return status.Band5.SSID
	}
	return ""
}

// signalFromMiWiFi 转换 MiWiFi API 的正值信号为负值 dBm
func signalFromMiWiFi(raw int) int {
	if raw > 0 && raw < 256 {
		return -raw
	}
	return raw
}

// getChannelForInterface 获取 WiFi 接口的当前信道
func getChannelForInterface(iface string) int {
	out, err := exec.Command("iwinfo", iface, "info").Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		// 行格式: "Channel: 36 (5.180 GHz)" 或 "Channel: 149 (5.745 GHz)"
		if strings.Contains(line, "Channel:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				fields := strings.Fields(strings.TrimSpace(parts[1]))
				if len(fields) > 0 {
					var ch int
					fmt.Sscanf(fields[0], "%d", &ch)
					return ch
				}
			}
		}
	}
	return 0
}

// getMockClients 返回 mock 客户端数据
func (c *ClientCollector) getMockClients() []ClientInfo {
	return []ClientInfo{
		{MAC: "9c:b8:b4:c6:bb:96", IP: "192.168.1.70", Name: "Midea_Smart_Device", SSID: "Wang.Tao_5G_Game", Band: "5.8G", Channel: 149, Signal: -55, ConnType: "wifi", Online: true, Interface: "wl0", Node: "主路由"},
		{MAC: "b0:6b:11:a6:3f:3c", IP: "192.168.1.182", Name: "98Q10L-TV", SSID: "Wang.Tao_5G_Game", Band: "5.8G", Channel: 149, Signal: -40, ConnType: "wifi", Online: true, Interface: "wl0", Node: "主路由"},
		{MAC: "50:88:11:f4:b3:b5", IP: "192.168.1.178", Name: "iPhone-15", SSID: "Wang.Tao_5G_Game", Band: "5.2G", Channel: 44, Signal: -58, ConnType: "wifi", Online: true, Interface: "wl2", Node: "书房Mesh"},
		{MAC: "54:ef:44:7e:f5:07", IP: "192.168.1.156", Name: "MacBook-Pro", SSID: "Wang.Tao_5G_Game", Band: "5.2G", Channel: 44, Signal: -63, ConnType: "wifi", Online: true, Interface: "wl2", Node: "书房Mesh"},
		{MAC: "e8:f6:0a:fc:b2:6c", IP: "192.168.1.95", Name: "esp32c6-fan-controller", SSID: "Wang.Tao", Band: "2.4G", Channel: 6, Signal: -42, ConnType: "wifi", Online: true, Interface: "wl1", Node: "主路由"},
		{MAC: "d8:d2:61:9d:cd:0e", IP: "192.168.1.88", Name: "midea_ac_0202", SSID: "Wang.Tao", Band: "2.4G", Channel: 6, Signal: -64, ConnType: "wifi", Online: true, Interface: "wl1", Node: "客厅Mesh"},
		{MAC: "ec:4d:3e:8a:22:b9", IP: "192.168.1.102", Name: "Xiaomi_Camera", SSID: "Wang.Tao", Band: "2.4G", Channel: 1, Signal: -59, ConnType: "wifi", Online: true, Interface: "wl1", Node: "客厅Mesh"},
		{MAC: "70:c9:32:c1:f5:80", IP: "192.168.1.115", Name: "dreame_vacuum_r9506", SSID: "Wang.Tao", Band: "2.4G", Channel: 1, Signal: -61, ConnType: "wifi", Online: true, Interface: "wl1", Node: "客厅Mesh"},
		{MAC: "aa:bb:cc:dd:ee:01", IP: "192.168.1.10", Name: "MyNAS", ConnType: "wired", Online: true, Node: "主路由"},
		{MAC: "aa:bb:cc:dd:ee:02", IP: "192.168.1.20", Name: "Desktop-PC", ConnType: "wired", Online: true, Node: "交换机"},
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
		if n, err := strconv.Atoi(fields[3]); err == nil {
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
					if n, err := strconv.Atoi(valFields[0]); err == nil {
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
