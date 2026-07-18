// Package rules 解析 SSTap-Rule 风格的 .rules 游戏路由规则文件。
//
// 真实格式（已用 FQrabbit/SSTap-Rule 实测验证，与早期假设的
// 「ip=/domain=/port=」INI 风格不同）：
//   - 第 1 行为 # 头注释：#<标题>,<中文显示>,<6 个标志位>,By-<来源>
//     例：#Genshin Impact TWHKMO,原神-港澳台服,0,0,1,0,1,0,By-ip_crawl_tool
//   - 其余每行为一个 IPv4 CIDR（如 18.172.52.0/24），可含 /32 /24 /16 /4 等；
//     空行与 # 注释行忽略。
//   - 向前兼容：若出现 domain= / port= 行也收集（当前主流规则库未使用）。
package rules

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// RuleFile 单个游戏解析后的规则。
type RuleFile struct {
	Key      string   // 文件名（去 .rules 后缀），稳定主键，用于 ipset 命名
	Title    string   // 头注释第 1 字段（英文/区服名）
	Subtitle string   // 头注释第 2 字段（中文显示名）
	Source   string   // 规则来源（By-xxx）
	CIDRs   []string // 已校验的 IP 段
	Domains []string // domain= 条目（向前兼容）
	Ports   []string // port= 条目（向前兼容）
	Warnings []string // 解析中被忽略/非法的行
}

// ParseRuleFile 解析单个 .rules 文本。name 为文件名（含或不含 .rules）。
func ParseRuleFile(name string, data []byte) (*RuleFile, error) {
	key := strings.TrimSuffix(filepath.Base(name), ".rules")
	rf := &RuleFile{Key: key}

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 支持超长行（部分规则文件行很长）
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if first {
				parseHeader(rf, line[1:])
			}
			// 其余 # 注释行忽略
			first = false
			continue
		}
		first = false
		switch {
		case strings.Contains(line, "="):
			k, v, _ := strings.Cut(line, "=")
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "domain":
				rf.Domains = append(rf.Domains, strings.TrimSpace(v))
			case "port":
				rf.Ports = append(rf.Ports, strings.TrimSpace(v))
			default:
				rf.Warnings = append(rf.Warnings, "unknown key: "+line)
			}
		default:
			cidr := line
			if !strings.Contains(cidr, "/") {
				cidr += "/32" // 裸 IP 视为 /32
			}
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				rf.Warnings = append(rf.Warnings, fmt.Sprintf("invalid cidr %q: %v", line, err))
				continue
			}
			rf.CIDRs = append(rf.CIDRs, cidr)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if rf.Title == "" {
		rf.Title = rf.Key
	}
	return rf, nil
}

// parseHeader 解析 # 头：#标题,中文显示,标志1..6,By-来源
func parseHeader(rf *RuleFile, h string) {
	parts := strings.Split(h, ",")
	rf.Title = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		rf.Subtitle = strings.TrimSpace(parts[1])
	}
	if len(parts) > 0 {
		last := strings.TrimSpace(parts[len(parts)-1])
		if strings.HasPrefix(last, "By-") {
			rf.Source = last
		}
	}
}

// ParseDir 解析目录下所有 .rules 文件，主键为文件名 stem。
func ParseDir(dir string) (map[string]*RuleFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*RuleFile)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".rules") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		rf, err := ParseRuleFile(e.Name(), data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		out[rf.Key] = rf
	}
	return out, nil
}
