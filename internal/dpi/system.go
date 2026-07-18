package dpi

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	flowCap        = 500 // flows 表最大条目，超出后删最旧
	flowPruneBatch = 100
	seenPruneMul   = 3 // seen 过期 = pollInterval * 该倍数未出现
)

// dnsEntry 一条域名解析关联记录。
type dnsEntry struct {
	domain  string
	ip      string
	expires time.Time
}

// SystemDetector 基于系统连接跟踪（conntrack）+ DNS 域名关联的真实 DPI 检测器。
// - conntrack 提供真实活跃流（src/dst/dport/proto）
// - AdGuard Home 查询日志提供「client -> 域名 -> IP」关联，用于区分同端口不同应用
type SystemDetector struct {
	BaseDetector
	cfg             DPIConfig
	conntrackTicker *time.Ticker
	dnsTicker       *time.Ticker
	stopChan        chan struct{}

	dnsMu  sync.RWMutex
	dnsMap map[string][]dnsEntry // clientIP -> 解析记录

	seenMu sync.Mutex
	seen   map[string]time.Time // 已推送流去重：flowKey -> 最近推送时间
}

// NewSystemDetector 创建系统级检测器。
func NewSystemDetector(cfg DPIConfig) *SystemDetector {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5
	}
	if cfg.DNS.TTL <= 0 {
		cfg.DNS.TTL = 300
	}
	return &SystemDetector{
		BaseDetector: *NewBaseDetector(),
		cfg:          cfg,
		stopChan:     make(chan struct{}),
		dnsMap:       make(map[string][]dnsEntry),
		seen:         make(map[string]time.Time),
	}
}

// Start 启动 conntrack 轮询；若启用 AdGuard 则同时启动 DNS 关联轮询。
func (d *SystemDetector) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running {
		return nil
	}
	d.running = true

	d.conntrackTicker = time.NewTicker(time.Duration(d.cfg.PollInterval) * time.Second)

	go d.conntrackLoop()

	if d.cfg.DNS.Source == "adguard" && d.cfg.DNS.AdGuardURL != "" {
		d.dnsTicker = time.NewTicker(time.Duration(maxInt(15, d.cfg.DNS.TTL/3)) * time.Second)
		go d.dnsLoop()
	} else {
		log.Printf("DPI: 未启用 DNS 域名关联（source=%q），仅端口级识别", d.cfg.DNS.Source)
	}
	return nil
}

// Stop 停止所有轮询。
func (d *SystemDetector) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.running {
		return nil
	}
	d.running = false
	d.conntrackTicker.Stop()
	if d.dnsTicker != nil {
		d.dnsTicker.Stop()
	}
	close(d.stopChan)
	return nil
}

// DetectPacket 系统检测器不做原始包检测，仅做连接级关联。
func (d *SystemDetector) DetectPacket(packet []byte) (*FlowInfo, error) {
	return nil, fmt.Errorf("system detector does not support raw packet detection")
}

// ---- conntrack 轮询 ----

func (d *SystemDetector) conntrackLoop() {
	d.pollConntrack() // 立即先跑一次，避免首屏空
	for {
		select {
		case <-d.stopChan:
			return
		case <-d.conntrackTicker.C:
			d.pollConntrack()
		}
	}
}

func (d *SystemDetector) pollConntrack() {
	lines, err := readConntrack()
	if err != nil {
		log.Printf("DPI: 读取连接跟踪失败: %v", err)
		return
	}

	now := time.Now()
	pruneBefore := now.Add(-time.Duration(seenPruneMul*d.cfg.PollInterval) * time.Second)
	dnsEnabled := d.cfg.DNS.Source == "adguard" && d.cfg.DNS.AdGuardURL != ""

	for _, line := range lines {
		src, dst, sport, dport, proto := parseConntrackLine(line)
		if dst == "" || dport == 0 {
			continue
		}

		// 1) 优先用 DNS 域名关联（更精确，可区分同端口不同站点）
		var domain, app string
		if dnsEnabled {
			domain = d.lookupDomain(src, dst)
		}
		if domain != "" {
			app = classifyByDomain(domain)
		}
		// 2) 回退端口级识别
		if app == "" {
			app = classifyByPort(dport)
		}
		if app == "" {
			continue
		}

		flow := &FlowInfo{
			ID:          d.nextFlowID(),
			SrcIP:       src,
			DstIP:       dst,
			SrcPort:     sport,
			DstPort:     dport,
			Protocol:    proto,
			Application: app,
			Detected:    true,
			DetectedAt:  now,
		}
		d.storeFlow(flow)

		// 通用协议（http/https/dns...）仅作展示，不触发 ipset 分流回调，
		// 避免把每个目标 IP 都加进 ipset 造成污染与风暴。
		if isGenericApp(app) {
			continue
		}

		// 命名应用：去重后仅向回调推送一次（seen 过期前不重复）。
		key := fmt.Sprintf("%s|%s|%d|%d|%s", src, dst, sport, dport, proto)
		d.seenMu.Lock()
		_, existed := d.seen[key]
		d.seen[key] = now
		d.seenMu.Unlock()
		if existed {
			continue
		}
		d.notifyCallbacks(flow)
	}

	// 清理过期 seen
	d.seenMu.Lock()
	for k, t := range d.seen {
		if t.Before(pruneBefore) {
			delete(d.seen, k)
		}
	}
	d.seenMu.Unlock()
}

// storeFlow 存入流表（供 GetAllFlows），超出上限时删最旧的一批。
func (d *SystemDetector) storeFlow(f *FlowInfo) {
	d.mu.Lock()
	d.flows[f.ID] = f
	if len(d.flows) > flowCap {
		ids := make([]uint64, 0, len(d.flows))
		for id := range d.flows {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids[:flowPruneBatch] {
			delete(d.flows, id)
		}
	}
	d.mu.Unlock()
}

// ---- DNS 关联轮询 ----

func (d *SystemDetector) dnsLoop() {
	d.refreshDNS()
	for {
		select {
		case <-d.stopChan:
			return
		case <-d.dnsTicker.C:
			d.refreshDNS()
		}
	}
}

func (d *SystemDetector) refreshDNS() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	records, err := fetchAdGuardQueryLog(ctx, d.cfg)
	if err != nil {
		log.Printf("DPI: 拉取 AdGuard 查询日志失败: %v", err)
		return
	}

	ttl := time.Duration(d.cfg.DNS.TTL) * time.Second
	now := time.Now()

	d.dnsMu.Lock()
	// 每次覆盖重建：未出现在本次记录中的条目自然过期（不再加入）。
	d.dnsMap = make(map[string][]dnsEntry)
	for _, r := range records {
		d.dnsMap[r.client] = append(d.dnsMap[r.client], dnsEntry{
			domain:  r.domain,
			ip:      r.ip,
			expires: now.Add(ttl),
		})
	}
	d.dnsMu.Unlock()
}

// lookupDomain 在 DNS 关联表中查找：clientIP 近期解析到的某个 IP 是否等于 dstIP。
func (d *SystemDetector) lookupDomain(clientIP, dstIP string) string {
	d.dnsMu.RLock()
	defer d.dnsMu.RUnlock()
	now := time.Now()
	for _, e := range d.dnsMap[clientIP] {
		if e.ip == dstIP && e.expires.After(now) {
			return e.domain
		}
	}
	return ""
}

// ---- conntrack 解析 ----

func readConntrack() ([]string, error) {
	const path = "/proc/net/nf_conntrack"
	if data, err := os.ReadFile(path); err == nil {
		return strings.Split(string(data), "\n"), nil
	}
	// 回退：conntrack -L 命令
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "conntrack", "-L").Output()
	if err != nil {
		return nil, fmt.Errorf("无法读取连接跟踪（/proc/net/nf_conntrack 不存在且 conntrack 命令不可用）: %w", err)
	}
	return strings.Split(string(out), "\n"), nil
}

// parseConntrackLine 解析一行 nf_conntrack，返回 src/dst/sport/dport/proto。
func parseConntrackLine(line string) (src, dst string, sport, dport uint16, proto string) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return
	}
	proto = fields[2] // 第 3 列：tcp/udp/icmp
	for _, f := range fields {
		switch {
		case strings.HasPrefix(f, "src="):
			src = strings.TrimPrefix(f, "src=")
		case strings.HasPrefix(f, "dst="):
			dst = strings.TrimPrefix(f, "dst=")
		case strings.HasPrefix(f, "sport="):
			sport = parsePort(strings.TrimPrefix(f, "sport="))
		case strings.HasPrefix(f, "dport="):
			dport = parsePort(strings.TrimPrefix(f, "dport="))
		}
	}
	return
}

func parsePort(s string) uint16 {
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return 0
	}
	return uint16(n)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
