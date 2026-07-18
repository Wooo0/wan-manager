package routing

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Wooo0/wan-manager/internal/dpi"
)

// cmdTimeout 外部命令（ipset/iptables/ip 等）的执行超时，防止工具卡死挂住管理器。
const cmdTimeout = 10 * time.Second

// runCmd 执行外部命令并带超时保护，返回命令错误由调用方处理。
func (m *Manager) runCmd(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

// Manager 策略路由管理器
type Manager struct {
	mu       sync.RWMutex
	config   *RoutingConfig
	wanTable map[string]int // WAN 名称 -> 路由表编号
	active   bool
	detector dpi.Detector
	appSets  map[string]string // app名称 -> ipset名称（动态维护）
}

// NewManager 创建策略路由管理器
func NewManager(config *RoutingConfig, wanInterfaces []string) *Manager {
	m := &Manager{
		config:   config,
		wanTable: make(map[string]int),
		appSets:  make(map[string]string),
	}

	// 为每个 WAN 口分配路由表编号（从 100 开始）
	for i, wan := range wanInterfaces {
		m.wanTable[wan] = 100 + i
	}

	return m
}

// SetDPIDetector 设置 DPI 检测器
func (m *Manager) SetDPIDetector(detector dpi.Detector) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.detector = detector
}

// GetDPIDetector 获取 DPI 检测器
func (m *Manager) GetDPIDetector() dpi.Detector {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.detector
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

	// 4. 启动 DPI（如果有应用规则且配置了检测器）
	if m.detector != nil && m.hasAppRules() {
		m.detector.RegisterCallback(m.onFlowDetected)
		if err := m.detector.Start(); err != nil {
			log.Printf("DPI 启动失败: %v", err)
		} else {
			log.Println("DPI 深度包检测已启动")
		}
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

	// 0. 停止 DPI
	if m.detector != nil {
		m.detector.Stop()
		log.Println("DPI 已停止")
	}

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

// GetConfigCopy 返回当前配置的深拷贝。
// 调用方（如 API 写入口）可安全地在副本上增删改规则后再传给 Reload，
// 不会与管理器内部持有的共享 config 指针产生 data race。
func (m *Manager) GetConfigCopy() *RoutingConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.config == nil {
		return nil
	}
	cp := *m.config
	if m.config.Rules != nil {
		cp.Rules = make([]Rule, len(m.config.Rules))
		copy(cp.Rules, m.config.Rules)
		// Rule 内部的切片也复制，避免共享底层数组
		for i := range cp.Rules {
			if m.config.Rules[i].IPs != nil {
				cp.Rules[i].IPs = append([]string(nil), m.config.Rules[i].IPs...)
			}
			if m.config.Rules[i].Apps != nil {
				cp.Rules[i].Apps = append([]string(nil), m.config.Rules[i].Apps...)
			}
		}
	}
	return &cp
}

// createIPSets 创建 ipset 集合
func (m *Manager) createIPSets() error {
	// 为每个 WAN 口创建 ipset
	for wan := range m.wanTable {
		setName := fmt.Sprintf("wan_%s", wan)
		// 先删除（忽略错误）
		m.runCmd("ipset", "destroy", setName)
		// 创建 hash:net 类型
		if err := m.runCmd("ipset", "create", setName, "hash:net"); err != nil {
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
		m.runCmd("ipset", "destroy", name)
		if err := m.runCmd("ipset", "create", name, "hash:net"); err != nil {
			return fmt.Errorf("创建 ipset %s 失败: %w", name, err)
		}
		// 添加 IP
		for _, ip := range ips {
			if err := m.runCmd("ipset", "add", name, ip); err != nil {
				log.Printf("添加 IP %s 到 %s 失败: %v", ip, name, err)
			}
		}
		log.Printf("创建 ipset: %s (%d 个 IP)", name, len(ips))
	}

	// 为自定义规则创建 ipset
	for _, rule := range m.config.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.Type != "custom" && rule.Type != "app" {
			continue
		}
		setName := fmt.Sprintf("rule_%s", sanitizeName(rule.Name))
		m.runCmd("ipset", "destroy", setName)
		if err := m.runCmd("ipset", "create", setName, "hash:ip"); err != nil {
			return fmt.Errorf("创建 ipset %s 失败: %w", setName, err)
		}
		if rule.Type == "custom" {
			for _, ip := range rule.IPs {
				if err := m.runCmd("ipset", "add", setName, ip); err != nil {
					log.Printf("添加 IP %s 到 %s 失败: %v", ip, setName, err)
				}
			}
			log.Printf("创建规则 ipset: %s (%d 个 IP)", setName, len(rule.IPs))
		} else if rule.Type == "app" {
			log.Printf("创建应用规则 ipset: %s (%d 个应用，动态填充)", setName, len(rule.Apps))
		}
	}

	return nil
}

// setupIPTables 配置 iptables 规则
func (m *Manager) setupIPTables() error {
	// 创建自定义链
	m.runCmd("iptables", "-t", "mangle", "-N", "WAN_MANAGER")
	m.runCmd("iptables", "-t", "mangle", "-F", "WAN_MANAGER")

	// 将自定义链插入到 PREROUTING
	m.runCmd("iptables", "-t", "mangle", "-I", "PREROUTING", "-j", "WAN_MANAGER")

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
		if err := m.runCmd("sh", "-c", cmd); err != nil {
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
		if err := m.runCmd("sh", "-c", cmd); err != nil {
			log.Printf("添加运营商分流规则失败: %v", err)
		}
	}

	return nil
}

// setupIPRules 配置 ip rule
func (m *Manager) setupIPRules() error {
	for wan, tableNum := range m.wanTable {
		// 添加 ip rule：匹配 mark 走对应路由表
		cmd := fmt.Sprintf("ip rule add fwmark %d table %d", tableNum, tableNum)
		if err := m.runCmd("sh", "-c", cmd); err != nil {
			log.Printf("添加 ip rule 失败: %v", err)
		}

		// 复制默认路由到自定义表
		// 获取 WAN 口的网关
		gateway := m.getWANGateway(wan)
		if gateway != "" {
			cmd = fmt.Sprintf("ip route add default via %s table %d", gateway, tableNum)
			if err := m.runCmd("sh", "-c", cmd); err != nil {
				log.Printf("配置路由表失败: %v", err)
			}
			log.Printf("配置路由表 %d: 默认网关 %s", tableNum, gateway)
		}
	}
	return nil
}

// getWANGateway 获取 WAN 口的网关
func (m *Manager) getWANGateway(wan string) string {
	// 从 ip route 获取默认网关
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", "route", "show", "dev", wan).Output()
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
		if err := m.runCmd("sh", "-c", cmd); err != nil {
			log.Printf("删除 ip rule 失败: %v", err)
		}
		cmd = fmt.Sprintf("ip route flush table %d 2>/dev/null", tableNum)
		if err := m.runCmd("sh", "-c", cmd); err != nil {
			log.Printf("清理路由表失败: %v", err)
		}
	}
}

// cleanupIPTables 清理 iptables 规则
func (m *Manager) cleanupIPTables() {
	m.runCmd("iptables", "-t", "mangle", "-D", "PREROUTING", "-j", "WAN_MANAGER")
	m.runCmd("iptables", "-t", "mangle", "-F", "WAN_MANAGER")
	m.runCmd("iptables", "-t", "mangle", "-X", "WAN_MANAGER")
}

// cleanupIPSets 清理 ipset
func (m *Manager) cleanupIPSets() {
	for wan := range m.wanTable {
		setName := fmt.Sprintf("wan_%s", wan)
		m.runCmd("ipset", "destroy", setName)
	}
	for _, name := range []string{"isp_telecom", "isp_unicom", "isp_mobile"} {
		m.runCmd("ipset", "destroy", name)
	}
	for _, rule := range m.config.Rules {
		setName := fmt.Sprintf("rule_%s", sanitizeName(rule.Name))
		m.runCmd("ipset", "destroy", setName)
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

// hasAppRules 检查是否有启用的应用规则
func (m *Manager) hasAppRules() bool {
	for _, rule := range m.config.Rules {
		if rule.Enabled && rule.Type == "app" && len(rule.Apps) > 0 {
			return true
		}
	}
	return false
}

// onFlowDetected DPI 流识别回调 - 根据应用规则动态添加 IP 到 ipset
func (m *Manager) onFlowDetected(flow *dpi.FlowInfo) {
	if !flow.Detected || flow.Application == "" {
		return
	}

	// 在锁内仅收集需要执行的规则，避免在持锁状态下执行阻塞式外部命令
	type ipsetTarget struct {
		setName string
		dstIP   string
		wan     string
	}
	var targets []ipsetTarget

	m.mu.RLock()
	for _, rule := range m.config.Rules {
		if !rule.Enabled || rule.Type != "app" {
			continue
		}

		matched := false
		for _, app := range rule.Apps {
			if app == flow.Application {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		setName := fmt.Sprintf("rule_%s", sanitizeName(rule.Name))
		tableNum := m.wanTable[rule.WAN]
		if tableNum == 0 {
			continue
		}
		targets = append(targets, ipsetTarget{
			setName: setName,
			dstIP:   flow.DstIP,
			wan:     rule.WAN,
		})
	}
	m.mu.RUnlock()

	// 锁外执行 ipset 命令，避免阻塞管理器的读路径
	for _, t := range targets {
		if err := m.runCmd("ipset", "add", t.setName, t.dstIP, "-exist"); err != nil {
			log.Printf("应用分流失败: %s -> %s (IP: %s): %v", flow.Application, t.wan, t.dstIP, err)
			continue
		}
		log.Printf("应用分流: %s -> %s (IP: %s)", flow.Application, t.wan, t.dstIP)
	}
}
