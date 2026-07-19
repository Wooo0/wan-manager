package isp

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Info 运营商信息
type Info struct {
	ISP     string `json:"isp"`     // 运营商：电信/联通/移动/其他
	Country string `json:"country"` // 国家
	Region  string `json:"region"`  // 地区
	City    string `json:"city"`    // 城市
	IP      string `json:"ip"`      // 公网 IP
}

// cachedInfo 带时间戳的缓存项
type cachedInfo struct {
	info *Info
	ts   time.Time
}

// Detector 运营商检测器
type Detector struct {
	mu       sync.RWMutex
	cache    map[string]cachedInfo // interface -> info
	cacheTTL time.Duration
}

// NewDetector 创建检测器
func NewDetector() *Detector {
	return &Detector{
		cache:    make(map[string]cachedInfo),
		cacheTTL: 5 * time.Minute,
	}
}

// Detect 检测指定 WAN 口的运营商信息（带 TTL 缓存，避免重拨换 IP 后长期不刷新）
func (d *Detector) Detect(iface string) *Info {
	d.mu.RLock()
	if c, ok := d.cache[iface]; ok && time.Since(c.ts) < d.cacheTTL {
		info := c.info
		d.mu.RUnlock()
		return info
	}
	d.mu.RUnlock()

	info := d.queryAll(iface)

	d.mu.Lock()
	d.cache[iface] = cachedInfo{info: info, ts: time.Now()}
	d.mu.Unlock()

	return info
}

// queryAll 查询 ipip.net（唯一可靠服务）
func (d *Detector) queryAll(iface string) *Info {
	info, err := d.queryIPIP(iface)
	if err == nil && info != nil {
		return info
	}

	log.Printf("IP 查询失败 (iface=%s): %v", iface, err)
	return &Info{
		ISP: "未知",
		IP:  "未知",
	}
}

// ipipResponse ipip.net JSON 响应结构
type ipipResponse struct {
	Ret  string `json:"ret"`
	Data struct {
		IP       string   `json:"ip"`
		Location []string `json:"location"`
	} `json:"data"`
}

func parseIPIPResponse(body []byte) (*Info, error) {
	var data ipipResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	if data.Ret != "ok" || len(data.Data.Location) < 5 {
		return nil, fmt.Errorf("ipip: invalid response")
	}
	return &Info{
		IP:      data.Data.IP,
		Country: data.Data.Location[0],
		Region:  data.Data.Location[1],
		City:    data.Data.Location[2],
		ISP:     detectISP(data.Data.Location[4]),
	}, nil
}

// queryIPIP 查询 ipip.net：
// 1. 优先 curl（--interface 绑定网卡）
// 2. 回退 wget（--bind-address 绑定网卡 IP）
// 3. 最后尝试 Go HTTP + SO_BINDTODEVICE（需 Linux，小米/OpenWrt 适用）
func (d *Detector) queryIPIP(iface string) (*Info, error) {
	// 方案 1: curl
	if info, err := d.queryIPIPWithCurl(iface); err == nil {
		return info, nil
	}

	// 方案 2: wget（小米/OpenWrt 通常自带，没有 curl）
	if info, err := d.queryIPIPWithWget(iface); err == nil {
		return info, nil
	}

	// 方案 3: Go HTTP + bind to interface（兜底）
	if info, err := d.queryIPIPWithHTTP(iface); err == nil {
		return info, nil
	}

	return nil, fmt.Errorf("所有 ipip.net 查询方式均失败（curl/wget/HTTP）")
}

func (d *Detector) queryIPIPWithCurl(iface string) (*Info, error) {
	cmd := exec.Command("curl", "-s", "--connect-timeout", "5", "--interface", iface,
		"https://myip.ipip.net/json")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseIPIPResponse(output)
}

func (d *Detector) queryIPIPWithWget(iface string) (*Info, error) {
	// wget 不支持 --interface，先用 ip 命令获取接口 IP，再用 --bind-address
	bindIP := getInterfaceIP(iface)
	args := []string{"-qO-", "--timeout=5", "https://myip.ipip.net/json"}
	if bindIP != "" {
		args = append([]string{"--bind-address=" + bindIP}, args...)
	}
	cmd := exec.Command("wget", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseIPIPResponse(output)
}

func (d *Detector) queryIPIPWithHTTP(iface string) (*Info, error) {
	transport := &http.Transport{}
	if err := bindTransportToInterface(transport, iface); err != nil {
		return nil, err
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get("https://myip.ipip.net/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseIPIPResponse(body)
}

// detectISP 从 ISP 字符串识别运营商
func detectISP(s string) string {
	s = strings.ToLower(s)
	if strings.Contains(s, "telecom") || strings.Contains(s, "电信") || strings.Contains(s, "chinatelecom") {
		return "电信"
	}
	if strings.Contains(s, "unicom") || strings.Contains(s, "联通") || strings.Contains(s, "chinaunicom") {
		return "联通"
	}
	if strings.Contains(s, "mobile") || strings.Contains(s, "移动") || strings.Contains(s, "chinamobile") {
		return "移动"
	}
	if strings.Contains(s, "cernet") || strings.Contains(s, "教育") {
		return "教育网"
	}
	if strings.Contains(s, "broadcast") || strings.Contains(s, "广电") {
		return "广电"
	}
	if strings.Contains(s, "railway") || strings.Contains(s, "铁通") {
		return "铁通"
	}
	if s != "" {
		return s
	}
	return "未知"
}

// ClearCache 清除缓存
func (d *Detector) ClearCache() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache = make(map[string]cachedInfo)
}

// getInterfaceIP 获取网卡的主 IPv4 地址（用于 wget --bind-address）
func getInterfaceIP(iface string) string {
	out, err := exec.Command("ip", "-4", "addr", "show", "dev", iface).Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				// 去掉 CIDR 后缀（如 192.168.1.1/24 → 192.168.1.1）
				ip := fields[1]
				if idx := strings.Index(ip, "/"); idx >= 0 {
					ip = ip[:idx]
				}
				return ip
			}
		}
	}
	return ""
}
