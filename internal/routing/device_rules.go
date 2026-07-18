package routing

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// DeviceRule 表示按源设备（MAC）指定的 WAN 转发策略。
// 小米等路由器在 mwan3 之外常通过独立 iptables 规则或 /etc/config/xq3 维护这类规则。
type DeviceRule struct {
	Name    string `json:"name"`    // 设备名称（尽可能从 comment/配置文件解析）
	MAC     string `json:"mac"`     // 源 MAC 地址，大写且用 : 分隔
	Policy  string `json:"policy"`  // 策略名，如 wan1_priority、wan_only、wan2_only
	WAN     string `json:"wan"`     // 解析后优先使用的 WAN 接口/别名
	Enabled bool   `json:"enabled"` // 是否启用
	Source  string `json:"source"`  // 规则来源：iptables / xq3 / miwifi / unknown
	Raw     string `json:"raw"`     // 原始规则文本，便于人工核对
}

// DeviceRuleSources 按优先级尝试解析源设备转发规则。
// 小米路由器的实际规则通常不在 /etc/config/mwan3 中，而是 iptables mangle 链或 xq3 配置。
func LoadDeviceWANRules() []DeviceRule {
	if rules := loadDeviceRulesFromXQ3(); len(rules) > 0 {
		return rules
	}
	if rules := loadDeviceRulesFromMWiFi(); len(rules) > 0 {
		return rules
	}
	if rules := loadDeviceRulesFromIPTables(); len(rules) > 0 {
		return rules
	}
	return nil
}

// loadDeviceRulesFromIPTables 从 iptables mangle 表中解析按 MAC 地址选路的规则。
// 典型规则形式：
//
//	-A MIWIFI_LB -m mac --mac-source 00:11:32:82:58:AF -j MARK --set-xmark 0x100/0xffffffff
//
// 也支持标准 mangle 链里出现的 -m mac --mac-source 规则。
func loadDeviceRulesFromIPTables() []DeviceRule {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "iptables", "-t", "mangle", "-L", "-n", "-v").Output()
	if err != nil {
		return nil
	}
	return parseDeviceRulesFromIPTables(string(out))
}

var (
	macRe     = regexp.MustCompile(`(?i)(?:(?:--mac-source\s+)|(?:MAC\s+))([0-9a-fA-F:]{17})`)
	commentRe = regexp.MustCompile(`(?i)(?:--comment\s+"([^"]+)"|/\*\s*([^\*]+)\s*\*/)`)
	setMarkRe = regexp.MustCompile(`(?i)MARK\s+set\s+(?:--set-mark\s+|--set-xmark\s+)?(0x[0-9a-fA-F]+|0x[0-9a-fA-F]+/0x[0-9a-fA-F]+|\d+)`)
	jumpRe    = regexp.MustCompile(`(?i)-j\s+([A-Za-z0-9_]+)`)
)

func parseDeviceRulesFromIPTables(output string) []DeviceRule {
	var rules []DeviceRule
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		macMatch := macRe.FindStringSubmatch(line)
		if len(macMatch) == 0 {
			continue
		}
		mac := normalizeMAC(macMatch[1])
		if mac == "" || seen[mac] {
			continue
		}
		seen[mac] = true

		comment := ""
		if cm := commentRe.FindStringSubmatch(line); len(cm) > 1 {
			for i := 1; i < len(cm); i++ {
				if cm[i] != "" {
					comment = strings.TrimSpace(cm[i])
					break
				}
			}
		}

		name := comment
		if name == "" {
			name = "设备 " + mac
		}

		policy := inferPolicyFromRule(line)
		wan := inferWANFromRule(line, policy)

		rules = append(rules, DeviceRule{
			Name:    name,
			MAC:     mac,
			Policy:  policy,
			WAN:     wan,
			Enabled: true,
			Source:  "iptables",
			Raw:     strings.TrimSpace(line),
		})
	}

	return rules
}

// inferPolicyFromRule 根据 iptables 规则里的 target 或 mark 推断策略名。
func inferPolicyFromRule(line string) string {
	if m := setMarkRe.FindStringSubmatch(line); len(m) > 1 {
		mark := strings.ToLower(m[1])
		if idx := strings.Index(mark, "/"); idx >= 0 {
			mark = mark[:idx]
		}
		switch mark {
		case "0x100", "0x1", "1":
			return "wan1_only"
		case "0x200", "0x2", "2":
			return "wan2_only"
		}
	}

	line = strings.ToLower(line)
	switch {
	case strings.Contains(line, "wan1"):
		return "wan1_priority"
	case strings.Contains(line, "wan2"):
		return "wan2_priority"
	case strings.Contains(line, "wan_only"):
		return "wan_only"
	default:
		if jm := jumpRe.FindStringSubmatch(line); len(jm) > 1 {
			return jm[1]
		}
		return "unknown"
	}
}

// inferWANFromRule 根据策略名/规则内容推断对应 WAN 名称（wan1/wan2）。
func inferWANFromRule(line, policy string) string {
	line = strings.ToLower(line)
	policy = strings.ToLower(policy)

	if strings.Contains(policy, "wan1") || strings.Contains(line, "wan1") {
		return "wan1"
	}
	if strings.Contains(policy, "wan2") || strings.Contains(line, "wan2") {
		return "wan2"
	}
	if strings.Contains(policy, "wan_only") && strings.Contains(line, "pppoe-wan_2") {
		return "wan2"
	}
	if strings.Contains(policy, "wan_only") {
		return "wan1"
	}
	return ""
}

// loadDeviceRulesFromXQ3 尝试读取小米 xq3 配置中的 device 规则。
// xq3 配置常见路径：/etc/config/xq3，常见格式（uci）：
//
//	config mac-binding 'xxx'
//	    option mac '00:11:32:82:58:af'
//	    option wan 'wan1'
//	    option comment 'FnOS'
func loadDeviceRulesFromXQ3() []DeviceRule {
	paths := []string{"/etc/config/xq3", "/etc/config/xq3_miwifi", "/etc/config/xq3-mac"}
	for _, p := range paths {
		if rules := loadDeviceRulesFromUCI(p); len(rules) > 0 {
			for i := range rules {
				rules[i].Source = "xq3"
			}
			return rules
		}
	}
	return nil
}

// loadDeviceRulesFromMWiFi 尝试读取 /etc/config/miwifi 中的源设备规则。
func loadDeviceRulesFromMWiFi() []DeviceRule {
	if rules := loadDeviceRulesFromUCI("/etc/config/miwifi"); len(rules) > 0 {
		for i := range rules {
			rules[i].Source = "miwifi"
		}
		return rules
	}
	return nil
}

func loadDeviceRulesFromUCI(path string) []DeviceRule {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var rules []DeviceRule
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var current DeviceRule
	var inSection bool

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "config ") {
			if inSection && current.MAC != "" {
				if current.Name == "" {
					current.Name = "设备 " + current.MAC
				}
				if current.Policy == "" {
					current.Policy = inferPolicyFromName(current.WAN)
				}
				rules = append(rules, current)
			}
			current = DeviceRule{Enabled: true}
			inSection = true
			continue
		}

		if !inSection {
			continue
		}

		if strings.HasPrefix(line, "option ") {
			parts := strings.SplitN(line, " ", 3)
			if len(parts) < 3 {
				continue
			}
			key := strings.TrimSpace(parts[1])
			val := strings.Trim(strings.TrimSpace(parts[2]), "'\"")
			switch key {
			case "mac":
				current.MAC = normalizeMAC(val)
			case "wan":
				current.WAN = val
			case "policy", "wan_policy":
				current.Policy = val
			case "name", "comment", "hostname":
				current.Name = val
			case "enabled":
				current.Enabled = val == "1" || strings.EqualFold(val, "true")
			}
		}
	}

	if inSection && current.MAC != "" {
		if current.Name == "" {
			current.Name = "设备 " + current.MAC
		}
		if current.Policy == "" {
			current.Policy = inferPolicyFromName(current.WAN)
		}
		rules = append(rules, current)
	}

	return rules
}

func inferPolicyFromName(wan string) string {
	wan = strings.ToLower(wan)
	if strings.Contains(wan, "wan2") {
		return "wan2_only"
	}
	if strings.Contains(wan, "wan1") || strings.Contains(wan, "wan") {
		return "wan1_only"
	}
	return "unknown"
}

func normalizeMAC(mac string) string {
	mac = strings.ToLower(strings.TrimSpace(mac))
	mac = strings.ReplaceAll(mac, "-", ":")
	parts := strings.Split(mac, ":")
	if len(parts) != 6 {
		return ""
	}
	for i, p := range parts {
		if len(p) == 1 {
			p = "0" + p
		}
		if len(p) != 2 {
			return ""
		}
		parts[i] = p
	}
	return strings.ToUpper(strings.Join(parts, ":"))
}

// DeviceRuleDisplayPolicy 把内部策略名翻译成用户能看懂的中文标签。
func DeviceRuleDisplayPolicy(policy, wan string) string {
	p := strings.ToLower(policy)
	w := strings.ToLower(wan)
	if strings.Contains(p, "priority") {
		if strings.Contains(p, "wan2") || w == "wan2" {
			return "WAN2 优先"
		}
		return "WAN1 优先"
	}
	// 明确指定 wan1_only / wan2_only
	if strings.Contains(p, "wan1_only") || w == "wan1" {
		return "WAN1 独占"
	}
	if strings.Contains(p, "wan2_only") || w == "wan2" {
		return "WAN2 独占"
	}
	// 通用 only（如本路由器的 mwan3 device 段：policy=only 且带具体接口名 wan/wan_2）
	if p == "only" && w != "" {
		return "固定走 " + wan
	}
	if strings.Contains(p, "only") {
		return "指定出口"
	}
	if strings.Contains(p, "balanced") || strings.Contains(p, "balance") {
		return "负载均衡"
	}
	if p == "" {
		return "负载均衡"
	}
	return policy
}

// LoadDeviceWANRulesFromMWAN3 从已解析的 mwan3 配置中提取“源设备 -> WAN”绑定。
// 小米/OpenWrt 多 WAN 路由器通常把这类规则写在 mwan3 的 device 段里，
// 由 mwan3 自动生成 ipset(mwan3_<iface>_devices) 与匹配规则，而不是独立的 iptables MAC 链或 xq3。
func LoadDeviceWANRulesFromMWAN3(cfg *MWAN3Config) []DeviceRule {
	if cfg == nil || len(cfg.Devices) == 0 {
		return nil
	}
	var rules []DeviceRule
	for _, dev := range cfg.Devices {
		mac := normalizeMAC(dev.MAC)
		name := dev.Name
		if name == "" {
			name = dev.Section
		}
		if name == "" {
			name = "设备 " + mac
		}
		wan := dev.Interface
		policy := "only"
		if wan == "" {
			policy = "unknown"
		}
		rules = append(rules, DeviceRule{
			Name:    name,
			MAC:     mac,
			Policy:  policy,
			WAN:     wan,
			Enabled: dev.Enabled,
			Source:  "mwan3",
			Raw:     fmt.Sprintf("device %s -> %s", dev.Section, wan),
		})
	}
	return rules
}

// DeviceRulePolicyOptions 返回前端下拉可展示的策略选项。
func DeviceRulePolicyOptions() []map[string]string {
	return []map[string]string{
		{"value": "wan1_priority", "label": "WAN1 优先"},
		{"value": "wan2_priority", "label": "WAN2 优先"},
		{"value": "wan1_only", "label": "WAN1 独占"},
		{"value": "wan2_only", "label": "WAN2 独占"},
		{"value": "balanced", "label": "负载均衡"},
	}
}
