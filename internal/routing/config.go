package routing

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// RoutingConfig 策略路由配置
type RoutingConfig struct {
	Enabled    bool       `toml:"enabled"`
	DefaultWAN string     `toml:"default_wan"`
	ISP        ISPConfig  `toml:"isp"`
	Rules      []Rule     `toml:"rules"`
}

// ISPConfig 运营商 IP 配置
type ISPConfig struct {
	Telecom []string `toml:"telecom"` // 电信 IP 段
	Unicom  []string `toml:"unicom"`  // 联通 IP 段
	Mobile  []string `toml:"mobile"`  // 移动 IP 段
}

// Rule 分流规则
type Rule struct {
	Name    string   `toml:"name"`
	Enabled bool     `toml:"enabled"`
	WAN     string   `toml:"wan"`
	Type    string   `toml:"type"` // custom, isp, app
	IPs     []string `toml:"ips"`
}

// DefaultRoutingConfig 返回默认配置
func DefaultRoutingConfig() *RoutingConfig {
	return &RoutingConfig{
		Enabled:    false,
		DefaultWAN: "wan1",
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
