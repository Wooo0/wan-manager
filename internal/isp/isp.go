package isp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
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

	// 获取接口 IP，创建绑定到该接口的 HTTP client
	localIP := getInterfaceIPv4(iface)
	client := newHTTPClient(localIP)

	info := d.queryAll(iface, client)

	d.mu.Lock()
	d.cache[iface] = info
	d.mu.Unlock()

	return info
}

// queryAll 并行查询多个服务
func (d *Detector) queryAll(iface string, client *http.Client) *Info {
	type result struct {
		info *Info
		err  error
	}

	ch := make(chan result, 3)

	go func() {
		info, err := d.queryIPIP(client)
		ch <- result{info, err}
	}()

	go func() {
		info, err := d.queryIPSB(client)
		ch <- result{info, err}
	}()

	go func() {
		info, err := d.queryIPW(client)
		ch <- result{info, err}
	}()

	for i := 0; i < 3; i++ {
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

// queryIPIP 查询 ipip.net
func (d *Detector) queryIPIP(client *http.Client) (*Info, error) {
	resp, err := client.Get("https://myip.ipip.net/json")
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
func (d *Detector) queryIPSB(client *http.Client) (*Info, error) {
	resp, err := client.Get("https://api.ip.sb/geoip")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		IP      string `json:"ip"`
		Country string `json:"country"`
		Region  string `json:"region"`
		City    string `json:"city"`
		ISP     string `json:"isp"`
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
func (d *Detector) queryIPW(client *http.Client) (*Info, error) {
	resp, err := client.Get("https://ipw.cn/api/ip/myip?json")
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

// newHTTPClient 创建 HTTP client，如果 localIP 不为空则绑定到该本地地址
func newHTTPClient(localIP string) *http.Client {
	transport := &http.Transport{}

	if localIP != "" {
		localAddr := &net.TCPAddr{IP: net.ParseIP(localIP)}
		dialer := &net.Dialer{
			LocalAddr: localAddr,
			Timeout:   5 * time.Second,
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}
}

// getInterfaceIPv4 获取指定网络接口的 IPv4 地址
func getInterfaceIPv4(iface string) string {
	cmd := exec.Command("ip", "addr", "show", iface)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "inet ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return strings.Split(parts[1], "/")[0]
			}
		}
	}

	return ""
}

// ClearCache 清除缓存
func (d *Detector) ClearCache() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cache = make(map[string]*Info)
}
