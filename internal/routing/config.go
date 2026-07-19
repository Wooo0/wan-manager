package routing

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// RoutingConfig 策略路由配置
type RoutingConfig struct {
	Enabled      bool         `toml:"enabled"`
	DefaultWAN   string       `toml:"default_wan"`
	BalanceMode  string       `toml:"balance_mode"`
	BalanceRatio string       `toml:"balance_ratio"`
	ISP          ISPConfig    `toml:"isp"`
	Rules        []Rule       `toml:"rules"`
	GameRulesDir string       `toml:"game_rules_dir"` // 游戏 .rules 目录；为空时默认 rules/game（相对于二进制）
	MWAN3Config  *MWAN3Config `json:"mwan3_config,omitempty"`
}

// ISPConfig 运营商分流配置
type ISPConfig struct {
	Enabled    bool              `toml:"enabled"`     // 是否启用运营商分流（按目的 IP 所属运营商选路）
	AutoDetect bool              `toml:"auto_detect"` // 启动时检测各 WAN 的运营商并自动映射（默认 true）
	WANMapping map[string]string `toml:"wan_mapping"` // 手动指定 WAN 名 -> 运营商(telecom/unicom/mobile)，覆盖/补充自动检测
	Telecom    []string          `toml:"telecom"`     // 电信 IP 段（可空，由加载器从公开源自动填充）
	Unicom     []string          `toml:"unicom"`      // 联通 IP 段
	Mobile     []string          `toml:"mobile"`      // 移动 IP 段
	Unmatched  string            `toml:"unmatched"`   // 未匹配到任何运营商时的出口：""=随机/默认路由，否则指定 WAN 名（如 wan1/wan2）
}

// Rule 分流规则
type Rule struct {
	Name    string   `toml:"name" json:"name"`
	Enabled bool     `toml:"enabled" json:"enabled"`
	WAN     string   `toml:"wan" json:"wan"`
	Type    string   `toml:"type" json:"type"` // custom, isp, app, game
	IPs     []string `toml:"ips" json:"ips"`
	Apps    []string `toml:"apps" json:"apps"` // 应用规则的应用列表
	Game    string   `toml:"game" json:"game"` // 游戏规则：对应的 .rules 文件名（去 .rules 后缀），其 CIDR 从该文件读取
}

// DefaultRoutingConfig 返回默认配置
func DefaultRoutingConfig() *RoutingConfig {
	return &RoutingConfig{
		Enabled:      false,
		DefaultWAN:   "wan1",
		BalanceMode:  "balanced",
		BalanceRatio: "1:1",
		ISP: ISPConfig{
			Enabled:    true,
			AutoDetect: true,
			WANMapping: map[string]string{},
			Telecom:    []string{},
			Unicom:     []string{},
			Mobile:     []string{},
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

// SaveRoutingConfig 将配置写入 TOML 文件
func SaveRoutingConfig(path string, cfg *RoutingConfig) error {
	// 只保存用户可控的字段（排除运行时注入的 MWAN3Config、ISP IP 段等）
	saveCfg := *cfg
	// 清除大数组避免写入巨量 IP 段（这些由远程/本地加载器管理）
	saveCfg.ISP.Telecom = nil
	saveCfg.ISP.Unicom = nil
	saveCfg.ISP.Mobile = nil
	saveCfg.MWAN3Config = nil

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建路由配置文件失败: %w", err)
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	if err := encoder.Encode(saveCfg); err != nil {
		return fmt.Errorf("写入路由配置文件失败: %w", err)
	}
	return nil
}
