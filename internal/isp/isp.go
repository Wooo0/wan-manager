package isp

import (
	"encoding/json"
	"fmt"
	"log"
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

// queryIPIP 查询 ipip.net（国内平台）
func (d *Detector) queryIPIP(iface string) (*Info, error) {
	cmd := exec.Command("curl", "-s", "--connect-timeout", "5", "--interface", iface, "https://myip.ipip.net/json")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var data struct {
		Ret  string `json:"ret"`
		Data struct {
			IP       string   `json:"ip"`
			Location []string `json:"location"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &data); err != nil {
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
