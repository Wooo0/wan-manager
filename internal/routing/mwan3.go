package routing

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

type MWAN3Config struct {
	Interfaces []MWAN3Interface `json:"interfaces"`
	Members    []MWAN3Member    `json:"members"`
	Policies   []MWAN3Policy    `json:"policies"`
	Rules      []MWAN3Rule      `json:"rules"`
	Devices    []MWAN3Device    `json:"devices"`
}

// MWAN3Device 表示 mwan3 的 device 段：按源设备 MAC 固定走某个 WAN 接口。
// 小米/OpenWrt 多 WAN 路由器通常把“源设备 -> WAN”绑定写在这里，
// 由 mwan3 自动生成 ipset(mwan3_<iface>_devices) 与匹配规则，而非独立 iptables MAC 链。
type MWAN3Device struct {
	Section   string `json:"section"`   // UCI section 名（通常为 MAC 去冒号形式）
	Name      string `json:"name"`      // 设备名（option name），如 lxc-tailscale / FnOS
	MAC       string `json:"mac"`       // 源 MAC 地址
	Interface string `json:"interface"` // 绑定的 WAN 接口，如 wan / wan_2
	Manual    bool   `json:"manual"`    // 是否手动指定（1=手动，0=自动学习）
	Family    string `json:"family"`    // ipv4 / ipv6
	Enabled   bool   `json:"enabled"`
}

type MWAN3Interface struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	TrackMethod string `json:"track_method"`
	TrackIP     string `json:"track_ip"`
	Metric      int    `json:"metric"`
}

type MWAN3Member struct {
	Name      string `json:"name"`
	Interface string `json:"interface"`
	Metric    int    `json:"metric"`
	Weight    int    `json:"weight"`
}

type MWAN3Policy struct {
	Name       string   `json:"name"`
	UseMembers []string `json:"use_members"`
	LastResort string   `json:"last_resort"`
}

type MWAN3Rule struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Src       string `json:"src"`
	Dst       string `json:"dst"`
	Proto     string `json:"proto"`
	UsePolicy string `json:"use_policy"`
}

func LoadMWAN3Config(path string) (*MWAN3Config, error) {
	config := &MWAN3Config{
		Interfaces: []MWAN3Interface{},
		Members:    []MWAN3Member{},
		Policies:   []MWAN3Policy{},
		Rules:      []MWAN3Rule{},
		Devices:    []MWAN3Device{},
	}

	if path == "" {
		path = "/etc/config/mwan3"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil
		}
		return nil, fmt.Errorf("读取mwan3配置失败: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var currentSection string
	var currentName string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "config ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				currentSection = parts[1]
				currentName = strings.Trim(parts[2], "'\"")
			}
			continue
		}

		if strings.HasPrefix(line, "option ") {
			parts := strings.SplitN(line, " ", 3)
			if len(parts) >= 3 {
				option := strings.TrimPrefix(parts[1], "option ")
				value := strings.Trim(parts[2], "'\"")
				config.addOption(currentSection, currentName, option, value)
			}
		}

		if strings.HasPrefix(line, "list ") {
			parts := strings.SplitN(line, " ", 3)
			if len(parts) >= 3 {
				listName := strings.TrimPrefix(parts[1], "list ")
				value := strings.Trim(parts[2], "'\"")
				config.addList(currentSection, currentName, listName, value)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("解析mwan3配置失败: %w", err)
	}

	return config, nil
}

func (c *MWAN3Config) addOption(section, name, option, value string) {
	switch section {
	case "interface":
		for i := range c.Interfaces {
			if c.Interfaces[i].Name == name {
				c.Interfaces[i].setOption(option, value)
				return
			}
		}
		iface := MWAN3Interface{Name: name}
		iface.setOption(option, value)
		c.Interfaces = append(c.Interfaces, iface)

	case "member":
		for i := range c.Members {
			if c.Members[i].Name == name {
				c.Members[i].setOption(option, value)
				return
			}
		}
		member := MWAN3Member{Name: name}
		member.setOption(option, value)
		c.Members = append(c.Members, member)

	case "policy":
		for i := range c.Policies {
			if c.Policies[i].Name == name {
				c.Policies[i].setOption(option, value)
				return
			}
		}
		policy := MWAN3Policy{Name: name}
		policy.setOption(option, value)
		c.Policies = append(c.Policies, policy)

	case "rule":
		for i := range c.Rules {
			if c.Rules[i].Name == name {
				c.Rules[i].setOption(option, value)
				return
			}
		}
		rule := MWAN3Rule{Name: name}
		rule.setOption(option, value)
		c.Rules = append(c.Rules, rule)
	case "device":
		for i := range c.Devices {
			if c.Devices[i].Section == name {
				c.Devices[i].setOption(option, value)
				return
			}
		}
		dev := MWAN3Device{Section: name, Enabled: true}
		dev.setOption(option, value)
		c.Devices = append(c.Devices, dev)
	}
}

func (c *MWAN3Config) addList(section, name, listName, value string) {
	switch section {
	case "policy":
		for i := range c.Policies {
			if c.Policies[i].Name == name {
				c.Policies[i].addList(listName, value)
				return
			}
		}
		policy := MWAN3Policy{Name: name}
		policy.addList(listName, value)
		c.Policies = append(c.Policies, policy)

	case "rule":
		for i := range c.Rules {
			if c.Rules[i].Name == name {
				c.Rules[i].addList(listName, value)
				return
			}
		}
		rule := MWAN3Rule{Name: name}
		rule.addList(listName, value)
		c.Rules = append(c.Rules, rule)
	}
}

func (i *MWAN3Interface) setOption(option, value string) {
	switch option {
	case "enabled":
		i.Enabled = value == "1"
	case "track_method":
		i.TrackMethod = value
	case "track_ip":
		i.TrackIP = value
	case "metric":
		if v, err := strconv.Atoi(value); err != nil {
			log.Printf("解析 MWAN3 接口 %s 的 metric 失败 (值=%q): %v", i.Name, value, err)
		} else {
			i.Metric = v
		}
	}
}

func (m *MWAN3Member) setOption(option, value string) {
	switch option {
	case "interface":
		m.Interface = value
	case "metric":
		if v, err := strconv.Atoi(value); err != nil {
			log.Printf("解析 MWAN3 成员 %s 的 metric 失败 (值=%q): %v", m.Interface, value, err)
		} else {
			m.Metric = v
		}
	case "weight":
		if v, err := strconv.Atoi(value); err != nil {
			log.Printf("解析 MWAN3 成员 %s 的 weight 失败 (值=%q): %v", m.Interface, value, err)
		} else {
			m.Weight = v
		}
	}
}

func (p *MWAN3Policy) setOption(option, value string) {
	switch option {
	case "last_resort":
		p.LastResort = value
	}
}

func (p *MWAN3Policy) addList(listName, value string) {
	switch listName {
	case "use_member":
		p.UseMembers = append(p.UseMembers, value)
	}
}

func (r *MWAN3Rule) setOption(option, value string) {
	switch option {
	case "enabled":
		r.Enabled = value == "1"
	case "src":
		r.Src = value
	case "dst":
		r.Dst = value
	case "proto":
		r.Proto = value
	case "use_policy":
		r.UsePolicy = value
	}
}

func (r *MWAN3Rule) addList(listName, value string) {
	switch listName {
	case "src":
		if r.Src == "" {
			r.Src = value
		} else {
			r.Src += ", " + value
		}
	case "dst":
		if r.Dst == "" {
			r.Dst = value
		} else {
			r.Dst += ", " + value
		}
	}
}

func (d *MWAN3Device) setOption(option, value string) {
	switch option {
	case "mac":
		d.MAC = value
	case "name":
		d.Name = value
	case "interface":
		d.Interface = value
	case "manual":
		d.Manual = value == "1"
	case "family":
		d.Family = value
	case "enabled":
		d.Enabled = value == "1" || strings.EqualFold(value, "true") || value == ""
	}
}

// GetWAN1WAN2Ratio 返回两个主要 WAN 接口的负载转发比例（基于 mwan3 balanced 策略的权重）。
// 返回 (wan1Name, wan1Weight, wan2Name, wan2Weight)。接口名直接取自 mwan3 实际配置
// （如 wan / wan_2），不再硬编码 wan1/wan2。
func GetWAN1WAN2Ratio(config *MWAN3Config) (string, int, string, int) {
	// 找出两个主要 WAN 接口（启用且非 IPv6 wan6*）
	var ifaces []string
	for _, iface := range config.Interfaces {
		if iface.Enabled && !strings.HasPrefix(strings.ToLower(iface.Name), "wan6") {
			ifaces = append(ifaces, iface.Name)
		}
		if len(ifaces) >= 2 {
			break
		}
	}
	if len(ifaces) < 2 {
		// 兜底：不排除未启用接口，保证总能取到两个
		for _, iface := range config.Interfaces {
			if iface.Name != "" && !strings.HasPrefix(strings.ToLower(iface.Name), "wan6") {
				ifaces = append(ifaces, iface.Name)
			}
		}
	}
	if len(ifaces) < 2 {
		return "", 1, "", 1
	}

	// 优先用 balanced 策略的权重计算负载比例（这才是“WAN 转发比例”的真实含义）
	if policy := findPolicy(config, "balanced"); policy != nil {
		w1 := weightSumForInterfaceInPolicy(config, policy, ifaces[0])
		w2 := weightSumForInterfaceInPolicy(config, policy, ifaces[1])
		if w1 > 0 && w2 > 0 {
			return ifaces[0], w1, ifaces[1], w2
		}
	}
	// 兜底：取每个接口的第一个有权重的成员
	return ifaces[0], memberWeightForInterface(config.Members, ifaces[0]),
		ifaces[1], memberWeightForInterface(config.Members, ifaces[1])
}

func findPolicy(config *MWAN3Config, name string) *MWAN3Policy {
	for i := range config.Policies {
		if config.Policies[i].Name == name {
			return &config.Policies[i]
		}
	}
	return nil
}

func weightSumForInterfaceInPolicy(config *MWAN3Config, policy *MWAN3Policy, iface string) int {
	total := 0
	for _, memberName := range policy.UseMembers {
		for _, m := range config.Members {
			if m.Name == memberName && m.Interface == iface && m.Weight > 0 {
				total += m.Weight
				break
			}
		}
	}
	return total
}

func memberWeightForInterface(members []MWAN3Member, iface string) int {
	for _, m := range members {
		if m.Interface == iface && m.Weight > 0 {
			return m.Weight
		}
	}
	return 1
}

func GetDefaultPolicy(config *MWAN3Config) *MWAN3Policy {
	for _, policy := range config.Policies {
		if policy.Name == "balanced" || policy.Name == "wan_only" || policy.Name == "wan_backup" {
			return &policy
		}
	}
	if len(config.Policies) > 0 {
		return &config.Policies[0]
	}
	return nil
}

func ParseIPRulesFromMWAN3(config *MWAN3Config) []Rule {
	var rules []Rule
	defaultPolicy := GetDefaultPolicy(config)
	if defaultPolicy == nil {
		return rules
	}

	for _, rule := range config.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.UsePolicy == defaultPolicy.Name {
			continue
		}

		var ips []string
		if rule.Dst != "" {
			ips = append(ips, rule.Dst)
		}
		if rule.Src != "" {
			ips = append(ips, rule.Src)
		}

		wans := make(map[string]bool)
		for _, memberName := range defaultPolicy.UseMembers {
			for _, member := range config.Members {
				if member.Name == memberName {
					wans[member.Interface] = true
				}
			}
		}

		var targetWAN string
		for _, memberName := range defaultPolicy.UseMembers {
			for _, member := range config.Members {
				if member.Name == memberName && wans[member.Interface] {
					targetWAN = member.Interface
					break
				}
			}
			if targetWAN != "" {
				break
			}
		}

		if len(ips) > 0 && targetWAN != "" {
			rules = append(rules, Rule{
				Name:    rule.Name,
				Enabled: rule.Enabled,
				WAN:     targetWAN,
				Type:    "custom",
				IPs:     ips,
			})
		}
	}

	return rules
}
