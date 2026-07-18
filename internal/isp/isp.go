package isp

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Info 运营商信息
type Info struct {
	ISP     string `json:"isp"`      // 运营商：电信/联通/移动/其他
	Country string `json:"country"`  // 国家
	Region  string `json:"region"`   // 地区
	City    string `json:"city"`     // 城市
	IP      string `json:"ip"`       // 公网 IP
}

// Detector 运营商检测器
type Detector struct {
	mu       sync.RWMutex
	cache    map[string]*Info // interface -> info
	client   *http.Client
}

// NewDetector 创建检测器
func NewDetector() *Detector {
	return &Detector{
		cache: make(map[string]*Info),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Detect 检测指定 WAN 口的运营商信息
func (d *Detector) Detect(iface string) *Info {
	d.mu.RLock()
	if info, ok := d.cache[iface]; ok {
		d.mu.RUnlock()
		return info
	}
	d.mu.RUnlock()

	// 并行请求多个 IP 查询服务
	info := d.queryAll(iface)

	d.mu.Lock()
	d.cache[iface] = info
	d.mu.Unlock()

	return info
}

// queryAll 并行查询多个服务
func (d *Detector) queryAll(iface string) *Info {
	type result struct {
		info *Info
		err  error
	}

	ch := make(chan result, 3)

	// ipip.net
	go func() {
		info, err := d.queryIPIP()
		ch <- result{info, err}
	}()

	// ip.sb
	go func() {
		info, err := d.queryIPSB()
		ch <- result{info, err}
	}()

	// ipw.cn
	go func() {
		info, err := d.queryIPW()
		ch <- result{info, err}
	}()

	// 等待第一个成功的结果
	for i := 0; i < 3; i++ {
		r := <-ch
		if r.err == nil && r.info != nil {
			return r.info
		}
	}

	log.Printf("所有 IP 查询服务均失败 (iface=%s)", iface)
	return &Info{
		ISP:  "未知",
		IP:   "未知",
	}
}

// queryIPIP 查询 ipip.net
func (d *Detector) queryIPIP() (*Info, error) {
	resp, err := d.client.Get("https://myip.ipip.net/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		Ret  string   `json:"ret"`
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	if data.Ret != "ok" || len(data.Data) < 4 {
		return nil, fmt.Errorf("ipip: invalid response")
	}

	return &Info{
		IP:      data.Data[0],
		Country: data.Data[1],
		Region:  data.Data[2],
		City:    data.Data[3],
		ISP:     detectISP(data.Data[3]),
	}, nil
}

// queryIPSB 查询 ip.sb
func (d *Detector) queryIPSB() (*Info, error) {
	resp, err := d.client.Get("https://api.ip.sb/geoip")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		IP       string `json:"ip"`
		Country  string `json:"country"`
		Region   string `json:"region"`
		City     string `json:"city"`
		ISP      string `json:"isp"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	return &Info{
		IP:      data.IP,
		Country: data.Country,
		Region:  data.Region,
		City:    data.City,
		ISP:     detectISP(data.ISP),
	}, nil
}

// queryIPW 查询 ipw.cn
func (d *Detector) queryIPW() (*Info, error) {
	resp, err := d.client.Get("https://ipw.cn/api/ip/myip?json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		IP  string `json:"ip"`
		ISP string `json:"isp"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	return &Info{
		IP:  data.IP,
		ISP: detectISP(data.ISP),
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
	if s != "" {
		return s
	}
	return "未知"
}

// ClearCache 清除缓存
func (d *Detector) ClearCache() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache = make(map[string]*Info)
}
