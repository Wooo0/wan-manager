package collector

import (
	"bufio"
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
	nameMap := collectDHCPNames()

	seen := make(map[string]bool)
	var result []ClientInfo

	for _, wc := range wifiClients {
		mac := normalizeMAC(wc.MAC)
		if seen[mac] {
			continue
		}
		seen[mac] = true

		if arp, ok := arpMap[mac]; ok {
			wc.IP = arp.IP
		}
		if name, ok := nameMap[mac]; ok {
			wc.Name = name
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
			Name:      nameMap[mac],
			ConnType:  "wired",
			Online:    true,
			Interface: arp.Device,
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
		if line == "" || strings.Contains(line, "MAC") || strings.Contains(line, "---") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		mac := fields[0]
		if !isValidMAC(mac) {
			continue
		}

		signal := 0
		for _, f := range fields {
			if strings.HasSuffix(f, "dBm") || strings.HasSuffix(f, "dB") {
				numStr := strings.TrimSuffix(f, "dBm")
				numStr = strings.TrimSuffix(numStr, "dB")
				numStr = strings.TrimSpace(numStr)
				if n, err := parseInt(numStr); err == nil {
					signal = n
					break
				}
			}
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
		return result
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum == 1 {
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			continue
		}

		ip := fields[0]
		hwType := fields[2]
		mac := fields[3]
		device := fields[5]

		if hwType != "0x1" {
			continue
		}
		if !isValidMAC(mac) {
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

	return result
}

func collectDHCPNames() map[string]string {
	result := make(map[string]string)

	data, err := os.ReadFile("/tmp/dhcp.leases")
	if err != nil {
		return result
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 {
			mac := normalizeMAC(fields[1])
			name := fields[3]
			if name != "*" && name != "" {
				result[mac] = name
			}
		}
	}

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
