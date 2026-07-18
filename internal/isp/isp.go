package isp

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
)

// Info 运营商信息
type Info struct {
	ISP     string `json:"isp"`     // 运营商：电信/联通/移动/其他
	Country string `json:"country"` // 国家
	Region  string `json:"region"`  // 地区
	City    string `json:"city"`    // 城市
	IP      string `json:"ip"`      // 公网 IP
}

// Detector 运营商检测器
type Detector struct {
	mu    sync.RWMutex
	cache map[string]*Info // interface -> info
}

// NewDetector 创建检测器
func NewDetector() *Detector {
	return &Detector{
		cache: make(map[string]*Info),
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

	info := d.queryAll(iface)

	d.mu.Lock()
	d.cache[iface] = info
	d.mu.Unlock()

	return info
}

// queryAll 并行查询多个国内服务
func (d *Detector) queryAll(iface string) *Info {
	type result struct {
		info *Info
		err  error
	}

	ch := make(chan result, 4)

	go func() {
		info, err := d.queryIPIP(iface)
		ch <- result{info, err}
	}()

	go func() {
		info, err := d.queryIPW(iface)
		ch <- result{info, err}
	}()

	go func() {
		info, err := d.queryIPCN(iface)
		ch <- result{info, err}
	}()

	go func() {
		info, err := d.queryIPAPI(iface)
		ch <- result{info, err}
	}()

	for i := 0; i < 4; i++ {
		r := <-ch
		if r.err == nil && r.info != nil {
			return r.info
		}
	}

	log.Printf("所有 IP 查询服务均失败 (iface=%s)", iface)
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
		Ret  string   `json:"ret"`
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(output, &data); err != nil {
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

// queryIPW 查询 ipw.cn（国内平台）
func (d *Detector) queryIPW(iface string) (*Info, error) {
	cmd := exec.Command("curl", "-s", "--connect-timeout", "5", "--interface", iface, "https://ipw.cn/api/ip/myip?json")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var data struct {
		IP  string `json:"ip"`
		ISP string `json:"isp"`
	}
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, err
	}

	if data.IP == "" {
		return nil, fmt.Errorf("ipw: empty ip")
	}

	return &Info{
		IP:  data.IP,
		ISP: detectISP(data.ISP),
	}, nil
}

// queryIPCN 查询 ip.cn（国内平台）
func (d *Detector) queryIPCN(iface string) (*Info, error) {
	cmd := exec.Command("curl", "-s", "--connect-timeout", "5", "--interface", iface, "https://ip.cn/api/index?ip=&type=0")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var data struct {
		Code int    `json:"code"`
		Data struct {
			IP      string `json:"ip"`
			Country string `json:"country"`
			Region  string `json:"region"`
			City    string `json:"city"`
			Isp     string `json:"isp"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &data); err != nil {
		return nil, err
	}

	if data.Code != 0 || data.Data.IP == "" {
		return nil, fmt.Errorf("ipcn: invalid response")
	}

	return &Info{
		IP:      data.Data.IP,
		Country: data.Data.Country,
		Region:  data.Data.Region,
		City:    data.Data.City,
		ISP:     detectISP(data.Data.Isp),
	}, nil
}

// queryIPAPI 查询 api.ipify.org（返回IP，再结合其他方式）
func (d *Detector) queryIPAPI(iface string) (*Info, error) {
	cmd := exec.Command("curl", "-s", "--connect-timeout", "5", "--interface", iface, "https://api.ipify.org")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	ip := strings.TrimSpace(string(output))
	if ip == "" {
		return nil, fmt.Errorf("ipify: empty ip")
	}

	return &Info{
		IP:  ip,
		ISP: "未知",
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
	d.cache = make(map[string]*Info)
}
