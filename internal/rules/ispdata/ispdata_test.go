package ispdata

import (
	"strings"
	"testing"
)

func TestParseCIDRList(t *testing.T) {
	in := "1.0.1.0/24\n1.2.3.4\n\n# comment\nbad\n10.0.0.0/8\n"
	cidrs, warns := ParseCIDRList(strings.NewReader(in))
	// 合法：1.0.1.0/24、1.2.3.4/32、10.0.0.0/8；"bad" 进 warnings
	if len(cidrs) != 3 {
		t.Fatalf("cidrs=%v want 3", cidrs)
	}
	if len(warns) != 1 {
		t.Fatalf("warns=%v want 1", warns)
	}
}

func TestNormalizeOperator(t *testing.T) {
	cases := map[string]string{
		"电信":         Telecom,
		"ChinaTelecom": Telecom,
		"联通":         Unicom,
		"中国联通":       Unicom,
		"移动":         Mobile,
		"China Mobile": Mobile,
		"未知":         "",
	}
	for in, want := range cases {
		if got := NormalizeOperator(in); got != want {
			t.Errorf("NormalizeOperator(%q)=%q want %q", in, got, want)
		}
	}
}

func TestLocalSnapshot(t *testing.T) {
	// 测试包自带 data/ 下的快照文件（随仓库分发，作为离线回退）
	for _, op := range []string{Telecom, Unicom, Mobile} {
		c, err := loadLocal(op, "data")
		if err != nil {
			t.Fatalf("%s local: %v", op, err)
		}
		if len(c) == 0 {
			t.Fatalf("%s local empty", op)
		}
	}
}
