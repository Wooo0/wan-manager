package routing

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
)

// Manager 策略路由管理器
type Manager struct {
	mu       sync.RWMutex
	config   *RoutingConfig
	wanTable map[string]int // WAN 名称 -> 路由表编号
	active   bool
}

// NewManager 创建策略路由管理器
func NewManager(config *RoutingConfig, wanInterfaces []string) *Manager {
	m := &Manager{
		config:   config,
		wanTable: make(map[string]int),
	}

	// 为每个 WAN 口分配路由表编号（从 100 开始）
	for i, wan := range wanInterfaces {
		m.wanTable[wan] = 100 + i
	}

	return m
}

// Start 启动策略路由
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.config.Enabled {
		log.Println("策略路由未启用")
		return nil
	}

	log.Println("启动策略路由...")

	// 1. 创建 ipset 集合
	if err := m.createIPSets(); err != nil {
		return fmt.Errorf("创建 ipset 失败: %w", err)
	}

	// 2. 配置 iptables 规则
	if err := m.setupIPTables(); err != nil {
		return fmt.Errorf("配置 iptables 失败: %w", err)
	}

	// 3. 配置 ip rule
	if err := m.setupIPRules(); err != nil {
		return fmt.Errorf("配置 ip rule 失败: %w", err)
	}

	m.active = true
	log.Println("策略路由启动成功")
	return nil
}

// Stop 停止策略路由
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.active {
		return nil
	}

	log.Println("停止策略路由...")

	// 1. 删除 ip rule
	m.cleanupIPRules()

	// 2. 删除 iptables 规则
	m.cleanupIPTables()

	// 3. 删除 ipset
	m.cleanupIPSets()

	m.active = false
	log.Println("策略路由已停止")
	return nil
}

// Reload 重新加载配置
func (m *Manager) Reload(config *RoutingConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 先停止
	if m.active {
		m.cleanupIPRules()
		m.cleanupIPTables()
		m.cleanupIPSets()
		m.active = false
	}

	// 更新配置
	m.config = config

	// 重新启动
	if config.Enabled {
		if err := m.createIPSets(); err != nil {
			return err
		}
		if err := m.setupIPTables(); err != nil {
			return err
		}
		if err := m.setupIPRules(); err != nil {
			return err
		}
		m.active = true
		log.Println("策略路由重新加载成功")
	}

	return nil
}

// IsActive 返回是否激活
func (m *Manager) IsActive() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// GetConfig 获取当前配置
func (m *Manager) GetConfig() *RoutingConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

// createIPSets 创建 ipset 集合
func (m *Manager) createIPSets() error {
	// 为每个 WAN 口创建 ipset
	for wan := range m.wanTable {
		setName := fmt.Sprintf("wan_%s", wan)
		// 先删除（忽略错误）
		exec.Command("ipset", "destroy", setName).Run()
		// 创建 hash:net 类型
		if err := exec.Command("ipset", "create", setName, "hash:net").Run(); err != nil {
			return fmt.Errorf("创建 ipset %s 失败: %w", setName, err)
		}
		log.Printf("创建 ipset: %s", setName)
	}

	// 为运营商 IP 创建 ipset
	ispSets := map[string][]string{
		"isp_telecom": m.config.ISP.Telecom,
		"isp_unicom":  m.config.ISP.Unicom,
		"isp_mobile":  m.config.ISP.Mobile,
	}

	for name, ips := range ispSets {
		exec.Command("ipset", "destroy", name).Run()
		if err := exec.Command("ipset", "create", name, "hash:net").Run(); err != nil {
			return fmt.Errorf("创建 ipset %s 失败: %w", name, err)
		}
		// 添加 IP
		for _, ip := range ips {
			if err := exec.Command("ipset", "add", name, ip).Run(); err != nil {
				log.Printf("添加 IP %s 到 %s 失败: %v", ip, name, err)
			}
		}
		log.Printf("创建 ipset: %s (%d 个 IP)", name, len(ips))
	}

	// 为自定义规则创建 ipset
	for _, rule := range m.config.Rules {
		if !rule.Enabled || rule.Type != "custom" {
			continue
		}
		setName := fmt.Sprintf("rule_%s", sanitizeName(rule.Name))
		exec.Command("ipset", "destroy", setName).Run()
		if err := exec.Command("ipset", "create", setName, "hash:net").Run(); err != nil {
			return fmt.Errorf("创建 ipset %s 失败: %w", setName, err)
		}
		for _, ip := range rule.IPs {
			if err := exec.Command("ipset", "add", setName, ip).Run(); err != nil {
				log.Printf("添加 IP %s 到 %s 失败: %v", ip, setName, err)
			}
		}
		log.Printf("创建规则 ipset: %s (%d 个 IP)", setName, len(rule.IPs))
	}

	return nil
}

// setupIPTables 配置 iptables 规则
func (m *Manager) setupIPTables() error {
	// 创建自定义链
	exec.Command("iptables", "-t", "mangle", "-N", "WAN_MANAGER").Run()
	exec.Command("iptables", "-t", "mangle", "-F", "WAN_MANAGER")

	// 将自定义链插入到 PREROUTING
	exec.Command("iptables", "-t", "mangle", "-I", "PREROUTING", "-j", "WAN_MANAGER")

	// 为每个自定义规则添加 MARK
	for _, rule := range m.config.Rules {
		if !rule.Enabled {
			continue
		}
		setName := fmt.Sprintf("rule_%s", sanitizeName(rule.Name))
		tableNum := m.wanTable[rule.WAN]
		if tableNum == 0 {
			log.Printf("未知的 WAN 口: %s", rule.WAN)
			continue
		}
		// 匹配 ipset 后打 mark
		cmd := fmt.Sprintf("iptables -t mangle -A WAN_MANAGER -m set --match-set %s dst -j MARK --set-mark %d", setName, tableNum)
		if err := exec.Command("sh", "-c", cmd).Run(); err != nil {
			return fmt.Errorf("添加 iptables 规则失败: %w", err)
		}
		log.Printf("添加规则: %s -> %s (mark %d)", rule.Name, rule.WAN, tableNum)
	}

	// 运营商分流规则
	ispWAN := map[string]string{
		"isp_telecom": "wan1", // 默认电信走 wan1
		"isp_unicom":  "wan2", // 默认联通走 wan2
		"isp_mobile":  "wan1", // 移动默认走 wan1
	}

	for setName, wan := range ispWAN {
		tableNum := m.wanTable[wan]
		if tableNum == 0 {
			continue
		}
		cmd := fmt.Sprintf("iptables -t mangle -A WAN_MANAGER -m set --match-set %s dst -j MARK --set-mark %d", setName, tableNum)
		exec.Command("sh", "-c", cmd).Run()
	}

	return nil
}

// setupIPRules 配置 ip rule
func (m *Manager) setupIPRules() error {
	for wan, tableNum := range m.wanTable {
		// 添加 ip rule：匹配 mark 走对应路由表
		cmd := fmt.Sprintf("ip rule add fwmark %d table %d", tableNum, tableNum)
		if err := exec.Command("sh", "-c", cmd).Run(); err != nil {
			log.Printf("添加 ip rule 失败: %v", err)
		}

		// 复制默认路由到自定义表
		// 获取 WAN 口的网关
		gateway := m.getWANGateway(wan)
		if gateway != "" {
			cmd = fmt.Sprintf("ip route add default via %s table %d", gateway, tableNum)
			exec.Command("sh", "-c", cmd).Run()
			log.Printf("配置路由表 %d: 默认网关 %s", tableNum, gateway)
		}
	}
	return nil
}

// getWANGateway 获取 WAN 口的网关
func (m *Manager) getWANGateway(wan string) string {
	// 从 ip route 获取默认网关
	out, err := exec.Command("ip", "route", "show", "dev", wan).Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "default via") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				return fields[2]
			}
		}
	}
	return ""
}

// cleanupIPRules 清理 ip rule
func (m *Manager) cleanupIPRules() {
	for _, tableNum := range m.wanTable {
		cmd := fmt.Sprintf("ip rule del fwmark %d table %d 2>/dev/null", tableNum, tableNum)
		exec.Command("sh", "-c", cmd).Run()
		cmd = fmt.Sprintf("ip route flush table %d 2>/dev/null", tableNum)
		exec.Command("sh", "-c", cmd).Run()
	}
}

// cleanupIPTables 清理 iptables 规则
func (m *Manager) cleanupIPTables() {
	exec.Command("iptables", "-t", "mangle", "-D", "PREROUTING", "-j", "WAN_MANAGER").Run()
	exec.Command("iptables", "-t", "mangle", "-F", "WAN_MANAGER").Run()
	exec.Command("iptables", "-t", "mangle", "-X", "WAN_MANAGER").Run()
}

// cleanupIPSets 清理 ipset
func (m *Manager) cleanupIPSets() {
	for wan := range m.wanTable {
		setName := fmt.Sprintf("wan_%s", wan)
		exec.Command("ipset", "destroy", setName).Run()
	}
	for _, name := range []string{"isp_telecom", "isp_unicom", "isp_mobile"} {
		exec.Command("ipset", "destroy", name).Run()
	}
	for _, rule := range m.config.Rules {
		setName := fmt.Sprintf("rule_%s", sanitizeName(rule.Name))
		exec.Command("ipset", "destroy", setName).Run()
	}
}

// sanitizeName 清理名称（用于 ipset 名称）
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "_")
	name = strings.ReplaceAll(name, "-", "_")
	// 只保留字母数字下划线
	var result []byte
	for _, c := range []byte(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			result = append(result, c)
		}
	}
	if len(result) > 20 {
		result = result[:20]
	}
	return string(result)
}
