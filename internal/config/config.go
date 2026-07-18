package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/Wooo0/wan-manager/internal/dpi"
)

// Config 主配置结构
type Config struct {
	Server    ServerConfig    `toml:"server"`
	Collector CollectorConfig `toml:"collector"`
	WAN       []WANConfig     `toml:"wan"`
	DPI       dpi.DPIConfig   `toml:"dpi"`
}

// ServerConfig 服务配置
type ServerConfig struct {
	ListenAddr string `toml:"listen_addr"`
}

// CollectorConfig 采集配置
type CollectorConfig struct {
	Interval int `toml:"interval"`
}

// WANConfig WAN 口配置
type WANConfig struct {
	Name      string `toml:"name"`
	Interface string `toml:"interface"`
	Label     string `toml:"label"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr: ":8899",
		},
		Collector: CollectorConfig{
			Interval: 1,
		},
		WAN: []WANConfig{
			{Name: "wan1", Interface: "pppoe-wan", Label: "WAN1"},
			{Name: "wan2", Interface: "pppoe-wan_2", Label: "WAN2"},
		},
		DPI: dpi.DefaultDPIConfig(),
	}
}

// Load 从文件加载配置
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return cfg, nil
}
