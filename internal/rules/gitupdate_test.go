package rules

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// buildSyntheticZip 构造一个模拟 FQrabbit/SSTap-Rule 归档结构的 zip：
// 顶层目录 SSTap-Rule-master/ 下含 rules/ 直接子级 .rules、子目录嵌套 .rules、以及无关文件。
func buildSyntheticZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	entries := []struct {
		name string
		body string
	}{
		{"SSTap-Rule-master/rules/Steam.rules", "#Steam,Steam,0,0,1,0,1,0,By-ip_crawl_tool\n1.2.3.0/24\n5.6.7.8/32\n"},
		{"SSTap-Rule-master/rules/GenshinImpact.exe_SSTAP.rules", "#Genshin,原神,0,0,1,0,1,0,By-ip_crawl_tool\n36.128.0.0/13\n"},
		{"SSTap-Rule-master/rules/sub/nested.rules", "9.9.9.0/24\n"}, // 子目录，应被忽略
		{"SSTap-Rule-master/README.md", "not a rules file\n"},                     // 无关文件，应被忽略
		{"SSTap-Rule-master/rules/NotRules.txt", "1.1.1.1/32\n"},                    // 非 .rules 后缀，应被忽略
	}
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatalf("write zip entry %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func TestExtractRulesFromZip(t *testing.T) {
	zipData := buildSyntheticZip(t)
	dir := t.TempDir()

	res, err := extractRulesFromZip(zipData, dir, "master", "https://example.com/archive.zip")
	if err != nil {
		t.Fatalf("extractRulesFromZip 失败: %v", err)
	}

	// 仅 2 个直接子级 .rules 应被提取；嵌套/非 .rules/无关文件被忽略
	if res.Count != 2 {
		t.Fatalf("期望提取 2 个 .rules，实际 %d (%v)", res.Count, res.Files)
	}
	want := map[string]bool{
		"Steam.rules":                    false,
		"GenshinImpact.exe_SSTAP.rules": false,
	}
	for _, f := range res.Files {
		if _, ok := want[f]; !ok {
			t.Fatalf("出现意外文件: %s", f)
		}
		want[f] = true
	}
	for f, seen := range want {
		if !seen {
			t.Fatalf("缺少期望文件: %s", f)
		}
	}

	// 校验文件确实写入磁盘且内容正确
	got, err := os.ReadFile(filepath.Join(dir, "Steam.rules"))
	if err != nil {
		t.Fatalf("读取提取文件失败: %v", err)
	}
	if !bytes.Contains(got, []byte("1.2.3.0/24")) {
		t.Fatalf("提取文件内容不正确: %s", got)
	}

	if res.Branch != "master" {
		t.Fatalf("branch 应为 master，实际 %s", res.Branch)
	}
	if len(res.Errors) != 0 {
		t.Fatalf("不应有错误，实际 %v", res.Errors)
	}
}
