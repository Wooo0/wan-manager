package collector

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	miwifiBaseURL     = "http://127.0.0.1/cgi-bin/luci"
	miwifiPublicKey   = "a2ffa5c9be07488bbb04a3a47d3c5f6a"
	miwifiLoginType   = "2"
	miwifiUsername     = "admin"
	miwifiLoginTimeout = 5 * time.Second
	miwifiAPITimeout   = 10 * time.Second
)

// MiWiFiDevice API 返回的单条设备
type MiWiFiDevice struct {
	MAC       string          `json:"mac"`
	OName     string          `json:"oname"`
	IsAP      int             `json:"isap"`
	Parent    string          `json:"parent"`
	Online    int             `json:"online"`
	Name      string          `json:"name"`
	Type      int             `json:"type"` // 0=有线 1=2.4G 2=5G 3=访客
	IP        []MiWiFiIPInfo  `json:"ip"`
	Push      int             `json:"push"`
}

// MiWiFiIPInfo 设备 IP 信息
type MiWiFiIPInfo struct {
	IP        string `json:"ip"`
	DownSpeed string `json:"downspeed"`
	UpSpeed   string `json:"upspeed"`
	Online    string `json:"online"`
	Active    int    `json:"active"`
}

// MiWiFiWiFiDevice wifi_connect_devices 返回
type MiWiFiWiFiDevice struct {
	MAC       string `json:"mac"`
	WiFiIndex int    `json:"wifiIndex"`
	Signal    int    `json:"signal"`
}

// MiWiFiNewStatus newstatus 返回
type MiWiFiNewStatus struct {
	Count    int    `json:"count"`
	ReCount  int    `json:"re_count"`
	CapCount int    `json:"cap_count"`
	Hardware struct {
		MAC      string `json:"mac"`
		Platform string `json:"platform"`
		Version  string `json:"version"`
	} `json:"hardware"`
	Band24G MiWiFiBandInfo `json:"2gh"`
	Band5G  MiWiFiBandInfo `json:"5gh"`
	Band5   MiWiFiBandInfo `json:"5g"`
}

// MiWiFiBandInfo 频段信息（含SSID）
type MiWiFiBandInfo struct {
	OnlineStaCount int    `json:"online_sta_count"`
	SSID           string `json:"ssid"`
}

// MiWiFiClient 小米路由器 API 客户端
type MiWiFiClient struct {
	password string
	token    string
	http     *http.Client
	mu       sync.Mutex
}

// NewMiWiFiClient 创建客户端
func NewMiWiFiClient(password string) *MiWiFiClient {
	if password == "" {
		return nil
	}
	return &MiWiFiClient{
		password: password,
		http: &http.Client{
			Timeout: miwifiAPITimeout,
		},
	}
}

// IsAvailable 检查是否可用
func (c *MiWiFiClient) IsAvailable() bool {
	return c != nil && c.password != ""
}

// Login 登录获取 stok token
func (c *MiWiFiClient) Login() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	nonce := c.generateNonce()
	pwdHash := c.calcPassword(nonce, c.password)

	form := url.Values{}
	form.Set("username", miwifiUsername)
	form.Set("logtype", miwifiLoginType)
	form.Set("password", pwdHash)
	form.Set("nonce", nonce)

	resp, err := c.http.PostForm(
		fmt.Sprintf("%s/api/xqsystem/login", miwifiBaseURL),
		form,
	)
	if err != nil {
		return fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Code  int    `json:"code"`
		Token string `json:"token"`
		Msg   string `json:"msg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析登录响应失败: %w", err)
	}
	if result.Token == "" {
		return fmt.Errorf("登录失败 code=%d msg=%s", result.Code, result.Msg)
	}

	c.token = result.Token
	log.Printf("MiWiFi 登录成功")
	return nil
}

// ensureToken 确保 token 有效，过期则重新登录
func (c *MiWiFiClient) ensureToken() error {
	if c.token != "" {
		return nil
	}
	return c.Login()
}

// GetDeviceList 获取全量设备列表
func (c *MiWiFiClient) GetDeviceList() ([]MiWiFiDevice, error) {
	if err := c.ensureToken(); err != nil {
		return nil, err
	}

	data, err := c.apiGet("misystem/devicelist")
	if err != nil {
		// token 过期，重试一次
		c.token = ""
		if err2 := c.Login(); err2 != nil {
			return nil, fmt.Errorf("重新登录失败: %w", err2)
		}
		data, err = c.apiGet("misystem/devicelist")
		if err != nil {
			return nil, err
		}
	}

	var result struct {
		List []MiWiFiDevice `json:"list"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析 devicelist 失败: %w", err)
	}

	return result.List, nil
}

// GetWiFiConnectDevices 获取 WiFi 设备信号强度
func (c *MiWiFiClient) GetWiFiConnectDevices() ([]MiWiFiWiFiDevice, error) {
	if err := c.ensureToken(); err != nil {
		return nil, err
	}

	data, err := c.apiGet("xqnetwork/wifi_connect_devices")
	if err != nil {
		return nil, err
	}

	var result struct {
		List []MiWiFiWiFiDevice `json:"list"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析 wifi_connect_devices 失败: %w", err)
	}

	return result.List, nil
}

// GetNewStatus 获取 newstatus（含 SSID、在线数等）
func (c *MiWiFiClient) GetNewStatus() (*MiWiFiNewStatus, error) {
	if err := c.ensureToken(); err != nil {
		return nil, err
	}

	data, err := c.apiGet("misystem/newstatus")
	if err != nil {
		return nil, err
	}

	// 先用通用 map 解析，兼容不同固件的 key 名（2gh/24gh/2g）
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析 newstatus 失败: %w", err)
	}

	result := &MiWiFiNewStatus{}
	if v, ok := getInt(raw, "count"); ok {
		result.Count = v
	}
	if v, ok := getInt(raw, "re_count"); ok {
		result.ReCount = v
	}
	// 遍历所有可能的频段 key
	for key, val := range raw {
		bi := parseBandInfo(val)
		if bi == nil {
			continue
		}
		switch key {
		case "2gh", "24gh", "24g", "2g":
			result.Band24G = *bi
		case "5gh", "5ghz":
			result.Band5G = *bi
		case "5g", "5gz":
			result.Band5 = *bi
		default:
			// 也可能有其他频段如 6gh，暂时忽略
		}
	}

	return result, nil
}

func getInt(m map[string]interface{}, key string) (int, bool) {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n), true
		case int:
			return n, true
		}
	}
	return 0, false
}

func parseBandInfo(v interface{}) *MiWiFiBandInfo {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	bi := &MiWiFiBandInfo{}
	if ssid, ok := m["ssid"].(string); ok {
		bi.SSID = ssid
	}
	if cnt, ok := m["online_sta_count"]; ok {
		switch n := cnt.(type) {
		case float64:
			bi.OnlineStaCount = int(n)
		case int:
			bi.OnlineStaCount = n
		}
	}
	return bi
}

// apiGet 通用 API GET 请求
func (c *MiWiFiClient) apiGet(path string) ([]byte, error) {
	url := fmt.Sprintf("%s/;stok=%s/api/%s", miwifiBaseURL, c.token, path)
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("API 请求失败 (%s): %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 检查返回的 code
	var check struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &check); err != nil {
		return nil, fmt.Errorf("解析 %s 响应失败: %w", path, err)
	}
	if check.Code != 0 {
		return nil, fmt.Errorf("%s 返回 code=%d msg=%s", path, check.Code, check.Msg)
	}

	return body, nil
}

// generateNonce 生成 nonce: 0_{MAC}_{timestamp}_{random}
func (c *MiWiFiClient) generateNonce() string {
	mac := c.getMAC()
	ts := time.Now().Unix()
	r, _ := rand.Int(rand.Reader, big.NewInt(1000))
	return fmt.Sprintf("0_%s_%d_%d", mac, ts, r.Int64())
}

// getMAC 获取 br-lan 的 MAC 地址（小写冒号格式）
func (c *MiWiFiClient) getMAC() string {
	out, err := exec.Command("cat", "/sys/class/net/br-lan/address").Output()
	if err == nil {
		raw := strings.TrimSpace(string(out))
		// 插入冒号: d4da21e625e6 → d4:da:21:e6:25:e6
		var parts []string
		for i := 0; i < len(raw); i += 2 {
			if i+2 <= len(raw) {
				parts = append(parts, raw[i:i+2])
			}
		}
		if len(parts) == 6 {
			return strings.Join(parts, ":")
		}
	}
	return "dc:ad:be:ef:00:01"
}

// calcPassword 计算密码哈希: SHA256(nonce + SHA256(password + public_key))
func (c *MiWiFiClient) calcPassword(nonce, password string) string {
	h1 := sha256.Sum256([]byte(password + miwifiPublicKey))
	h2 := sha256.Sum256([]byte(nonce + fmt.Sprintf("%x", h1)))
	return fmt.Sprintf("%x", h2)
}
