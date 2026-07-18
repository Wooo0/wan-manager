package rules

import (
	"os"
	"testing"
)

// 用仓库 testdata 下真实拉取的 SSTap-Rule 样本做端到端校验。
func TestParseRuleFile_Genshin(t *testing.T) {
	data, err := os.ReadFile("testdata/GenshinImpact.exe_SSTAP.rules")
	if err != nil {
		t.Fatal(err)
	}
	rf, err := ParseRuleFile("GenshinImpact.exe_SSTAP.rules", data)
	if err != nil {
		t.Fatal(err)
	}
	if rf.Title != "Genshin Impact TWHKMO" {
		t.Errorf("Title=%q", rf.Title)
	}
	if rf.Subtitle != "原神-港澳台服" {
		t.Errorf("Subtitle=%q", rf.Subtitle)
	}
	if rf.Source != "By-ip_crawl_tool" {
		t.Errorf("Source=%q", rf.Source)
	}
	if len(rf.CIDRs) != 9 {
		t.Errorf("CIDRs=%d want 9", len(rf.CIDRs))
	}
	if len(rf.Warnings) != 0 {
		t.Errorf("Warnings=%v", rf.Warnings)
	}
}

func TestParseRuleFile_Steam(t *testing.T) {
	data, err := os.ReadFile("testdata/Steam.rules")
	if err != nil {
		t.Fatal(err)
	}
	rf, err := ParseRuleFile("Steam.rules", data)
	if err != nil {
		t.Fatal(err)
	}
	if rf.Title != "Steam" || rf.Subtitle != "Steam-社区(Beta)" || rf.Source != "By-FQrabbit" {
		t.Errorf("header mismatch: %+v", rf)
	}
	if len(rf.CIDRs) != 43 {
		t.Errorf("CIDRs=%d want 43", len(rf.CIDRs))
	}
	if !contains(rf.CIDRs, "23.32.248.26/32") {
		t.Errorf("missing known CIDR 23.32.248.26/32")
	}
	if len(rf.Warnings) != 0 {
		t.Errorf("Warnings=%v", rf.Warnings)
	}
}

func TestParseRuleFile_ValOrant(t *testing.T) {
	data, err := os.ReadFile("testdata/Valorant.rules")
	if err != nil {
		t.Fatal(err)
	}
	rf, err := ParseRuleFile("Valorant.rules", data)
	if err != nil {
		t.Fatal(err)
	}
	if rf.Title != "Valorant" || rf.Subtitle != "无畏契约" || rf.Source != "By-小明" {
		t.Errorf("header mismatch: %+v", rf)
	}
	if len(rf.CIDRs) == 0 {
		t.Fatalf("no CIDRs parsed")
	}
	if !contains(rf.CIDRs, "3.0.0.0/4") { // 非常规前缀长度也要认
		t.Errorf("missing CIDR with /4 prefix")
	}
}

func TestParseDir(t *testing.T) {
	m, err := ParseDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 4 {
		t.Fatalf("parsed games=%d want 4 (got keys: %v)", len(m), keysOf(m))
	}
	for _, k := range []string{
		"GenshinImpact.exe_SSTAP",
		"PlayerUnknown's-Battlegrounds-update",
		"Steam",
		"Valorant",
	} {
		rf, ok := m[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if len(rf.CIDRs) == 0 {
			t.Errorf("game %q has no CIDRs", k)
		}
	}
}

// 合成用例：验证头解析、裸 IP→/32、非法 CIDR 进 Warnings、domain=/port= 向前兼容。
func TestParseRuleFile_Synthetic(t *testing.T) {
	in := "#My Game,我的游戏,0,0,1,0,0,0,By-test\n" +
		"1.2.3.0/24\n" +
		"bad.line.here\n" +
		"domain=example.com\n" +
		"port=443\n" +
		"\n" +
		"# comment\n" +
		"9.9.9.9\n"
	rf, err := ParseRuleFile("mygame.rules", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if rf.Title != "My Game" || rf.Subtitle != "我的游戏" || rf.Source != "By-test" {
		t.Errorf("header mismatch: %+v", rf)
	}
	if len(rf.CIDRs) != 2 { // 1.2.3.0/24 + 9.9.9.9/32
		t.Errorf("CIDRs=%v want 2", rf.CIDRs)
	}
	if len(rf.Domains) != 1 || rf.Domains[0] != "example.com" {
		t.Errorf("Domains=%v", rf.Domains)
	}
	if len(rf.Ports) != 1 || rf.Ports[0] != "443" {
		t.Errorf("Ports=%v", rf.Ports)
	}
	if len(rf.Warnings) != 1 { // bad.line.here 非法
		t.Errorf("Warnings=%v want 1", rf.Warnings)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func keysOf(m map[string]*RuleFile) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
