// Package ispdata 加载运营商 IP 段（电信/联通/移动），
// 优先从公开源拉取最新数据，失败时回退到随程序分发的本地快照目录。
package ispdata

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 运营商规范键（与 routing 包、配置 wan_mapping 保持一致）
const (
	Telecom = "telecom"
	Unicom  = "unicom"
	Mobile  = "mobile"
)

// DefaultLocalDir 返回内置快照的默认本地目录：可执行文件同级的 data/isp。
// 该目录随程序分发，作为离线/远程失败时的回退。
func DefaultLocalDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "data", "isp")
	}
	return "data/isp"
}

// remoteSources 各运营商的远程地址（依次尝试，任一成功即用）。
// 首选 metowolf 的 github.io 镜像（CDN、每小时更新），其次 raw.githubusercontent。
func remoteSources() map[string][]string {
	return map[string][]string{
		Telecom: {
			"https://metowolf.github.io/iplist/data/isp/chinatelecom.txt",
			"https://raw.githubusercontent.com/metowolf/iplist/master/data/isp/chinatelecom.txt",
		},
		Unicom: {
			"https://metowolf.github.io/iplist/data/isp/chinaunicom.txt",
			"https://raw.githubusercontent.com/metowolf/iplist/master/data/isp/chinaunicom.txt",
		},
		Mobile: {
			"https://metowolf.github.io/iplist/data/isp/chinamobile.txt",
			"https://raw.githubusercontent.com/metowolf/iplist/master/data/isp/chinamobile.txt",
		},
	}
}

// Source 表示运营商 IP 段的加载来源。
type Source string

const (
	SourceRemote Source = "remote" // 远程最新（metowolf github）
	SourceLocal  Source = "local"  // 本地快照（随程序分发）
	SourceEmpty  Source = "empty"  // 远程与本地均不可用，未加载到任何段
)

// LoadResult 加载结果：既包含实际数据，也记录每个运营商的来源与错误，
// 供 Web 面板展示「三网各加载多少条、来自远程还是本地快照」。
type LoadResult struct {
	Data    map[string][]string // 运营商 -> IP 段（用于合并进配置）
	Sources map[string]Source    // 运营商 -> 来源（remote/local/empty）
	Errors  []error             // 完全失败的运营商错误
}

// LoadDefaults 加载三网运营商 IP 段：
// 优先远程最新数据，失败时回退到本地快照目录（随程序分发）。
func LoadDefaults() *LoadResult {
	return Load(remoteSources(), DefaultLocalDir())
}

// Load 按给定远程源加载运营商 IP 段；任一运营商失败时回退到 localDir 下的 <op>.txt。
// 任一运营商彻底不可用不影响其它运营商，并在 Sources 中标记为 empty。
func Load(sources map[string][]string, localDir string) *LoadResult {
	res := &LoadResult{
		Data:    map[string][]string{},
		Sources: map[string]Source{},
	}
	for op, urls := range sources {
		cidrs, src, err := loadOne(op, urls, localDir)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w", op, err))
			res.Sources[op] = SourceEmpty
			continue
		}
		res.Data[op] = cidrs
		res.Sources[op] = src
	}
	return res
}

func loadOne(op string, urls []string, localDir string) ([]string, Source, error) {
	for _, u := range urls {
		cidrs, err := download(u)
		if err == nil && len(cidrs) > 0 {
			log.Printf("运营商 IP 段(%s): 远程加载 %d 条 (%s)", op, len(cidrs), u)
			return cidrs, SourceRemote, nil
		}
		if err != nil {
			log.Printf("运营商 IP 段(%s) 远程获取失败，尝试下一个源: %v", op, err)
		}
	}
	// 回退本地快照
	if b, e := loadLocal(op, localDir); e == nil && len(b) > 0 {
		log.Printf("运营商 IP 段(%s): 使用本地快照 %d 条 (%s)", op, len(b), localDir)
		return b, SourceLocal, nil
	}
	return nil, SourceEmpty, fmt.Errorf("远程与本地快照均不可用")
}

func download(url string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	cidrs, warns := ParseCIDRList(resp.Body)
	for _, w := range warns {
		log.Printf("运营商 IP 段解析警告: %v", w)
	}
	return cidrs, nil
}

func loadLocal(op, dir string) ([]string, error) {
	f, err := os.Open(filepath.Join(dir, op+".txt"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cidrs, warns := ParseCIDRList(f)
	for _, w := range warns {
		log.Printf("运营商 IP 段解析警告: %v", w)
	}
	return cidrs, nil
}

// ParseCIDRList 解析 IP 段列表：每行一个 CIDR 或单个 IP（自动补 /32）。
// 忽略空行与 # 注释；非法行计入 warnings 但不致命。
func ParseCIDRList(r io.Reader) ([]string, []error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, []error{err}
	}
	var out []string
	var warns []error
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cidr, w := normalizeCIDR(line)
		if w != nil {
			warns = append(warns, w)
			continue
		}
		out = append(out, cidr)
	}
	return out, warns
}

func normalizeCIDR(s string) (string, error) {
	if _, _, err := net.ParseCIDR(s); err == nil {
		return s, nil
	}
	if ip := net.ParseIP(s); ip != nil {
		return s + "/32", nil
	}
	return "", fmt.Errorf("非法 IP/CIDR: %q", s)
}

// NormalizeOperator 将运营商名称（中文或英文）规范为内部键。
func NormalizeOperator(name string) string {
	s := strings.ToLower(name)
	switch {
	case strings.Contains(s, "telecom"), strings.Contains(s, "电信"):
		return Telecom
	case strings.Contains(s, "unicom"), strings.Contains(s, "联通"):
		return Unicom
	case strings.Contains(s, "mobile"), strings.Contains(s, "移动"):
		return Mobile
	}
	return ""
}
