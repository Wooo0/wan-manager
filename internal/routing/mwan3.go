package routing

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type MWAN3Config struct {
	Interfaces []MWAN3Interface `json:"interfaces"`
	Members    []MWAN3Member    `json:"members"`
	Policies   []MWAN3Policy    `json:"policies"`
	Rules      []MWAN3Rule      `json:"rules"`
}

type MWAN3Interface struct {
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	TrackMethod   string `json:"track_method"`
	TrackIP       string `json:"track_ip"`
	Metric        int    `json:"metric"`
}

type MWAN3Member struct {
	Name      string `json:"name"`
	Interface string `json:"interface"`
	Metric    int    `json:"metric"`
	Weight    int    `json:"weight"`
}

type MWAN3Policy struct {
	Name        string   `json:"name"`
	UseMembers  []string `json:"use_members"`
	LastResort  string   `json:"last_resort"`
}

type MWAN3Rule struct {
	Name       string   `json:"name"`
	Enabled    bool     `json:"enabled"`
	Src        string   `json:"src"`
	Dst        string   `json:"dst"`
	Proto      string   `json:"proto"`
	UsePolicy  string   `json:"use_policy"`
}

func LoadMWAN3Config(path string) (*MWAN3Config, error) {
	config := &MWAN3Config{
		Interfaces: []MWAN3Interface{},
		Members:    []MWAN3Member{},
		Policies:   []MWAN3Policy{},
		Rules:      []MWAN3Rule{},
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
		i.Metric, _ = strconv.Atoi(value)
	}
}

func (m *MWAN3Member) setOption(option, value string) {
	switch option {
	case "interface":
		m.Interface = value
	case "metric":
		m.Metric, _ = strconv.Atoi(value)
	case "weight":
		m.Weight, _ = strconv.Atoi(value)
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

func GetBalanceRatio(config *MWAN3Config) map[string]int {
	ratio := make(map[string]int)
	for _, member := range config.Members {
		if member.Weight > 0 {
			ratio[member.Interface] = member.Weight
		}
	}
	return ratio
}

func GetWAN1WAN2Ratio(config *MWAN3Config) (int, int) {
	wan1Weight := 1
	wan2Weight := 1
	for _, member := range config.Members {
		if strings.Contains(member.Interface, "wan1") || member.Interface == "wan" {
			if member.Weight > 0 {
				wan1Weight = member.Weight
			}
		}
		if strings.Contains(member.Interface, "wan2") {
			if member.Weight > 0 {
				wan2Weight = member.Weight
			}
		}
	}
	return wan1Weight, wan2Weight
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