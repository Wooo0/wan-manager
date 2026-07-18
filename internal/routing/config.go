package routing

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// RoutingConfig 策略路由配置
type RoutingConfig struct {
	Enabled        bool       `toml:"enabled"`
	DefaultWAN     string     `toml:"default_wan"`
	BalanceMode    string     `toml:"balance_mode"`
	BalanceRatio   string     `toml:"balance_ratio"`
	ISP            ISPConfig  `toml:"isp"`
	Rules          []Rule     `toml:"rules"`
	MWAN3Config    *MWAN3Config `json:"mwan3_config,omitempty"`
}

// ISPConfig 运营商 IP 配置
type ISPConfig struct {
	Telecom []string `toml:"telecom"` // 电信 IP 段
	Unicom  []string `toml:"unicom"`  // 联通 IP 段
	Mobile  []string `toml:"mobile"`  // 移动 IP 段
}

// Rule 分流规则
type Rule struct {
	Name    string   `toml:"name" json:"name"`
	Enabled bool     `toml:"enabled" json:"enabled"`
	WAN     string   `toml:"wan" json:"wan"`
	Type    string   `toml:"type" json:"type"` // custom, isp, app
	IPs     []string `toml:"ips" json:"ips"`
	Apps    []string `toml:"apps" json:"apps"` // 应用规则的应用列表
}

// DefaultRoutingConfig 返回默认配置
func DefaultRoutingConfig() *RoutingConfig {
	return &RoutingConfig{
		Enabled:      false,
		DefaultWAN:   "wan1",
		BalanceMode:  "balanced",
		BalanceRatio: "1:1",
		ISP: ISPConfig{
			Telecom: []string{},
			Unicom:  []string{},
			Mobile:  []string{},
		},
		Rules: []Rule{},
	}
}

// LoadRoutingConfig 加载策略路由配置
func LoadRoutingConfig(path string) (*RoutingConfig, error) {
	cfg := DefaultRoutingConfig()

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("读取路由配置文件失败: %w", err)
	}

	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, fmt.Errorf("解析路由配置文件失败: %w", err)
	}

	return cfg, nil
}
