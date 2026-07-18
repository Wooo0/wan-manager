package routing

import (
	"os"
	"strings"
	"testing"
)

func TestParseDeviceRulesFromIPTables(t *testing.T) {
	output := `
Chain MIWIFI_LB (1 references)
 pkts bytes target     prot opt in     out     source               destination
    0     0 MARK       all  --  *      *       0.0.0.0/0            0.0.0.0/0           MAC 00:11:32:82:58:AF MARK set 0x100
    0     0 MARK       all  --  *      *       0.0.0.0/0            0.0.0.0/0           MAC BC:24:11:7B:6D:71 MARK set 0x200 /* FnOS */
    0     0 MARK       all  --  *      *       0.0.0.0/0            0.0.0.0/0           MAC 3C:7C:3F:20:2F:06 MARK set 0x100/0xffffffff /* lxc-tailscale */
`

	rules := parseDeviceRulesFromIPTables(output)
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}

	cases := []struct {
		idx    int
		name   string
		mac    string
		policy string
		wan    string
	}{
		{0, "设备 00:11:32:82:58:AF", "00:11:32:82:58:AF", "wan1_only", "wan1"},
		{1, "FnOS", "BC:24:11:7B:6D:71", "wan2_only", "wan2"},
		{2, "lxc-tailscale", "3C:7C:3F:20:2F:06", "wan1_only", "wan1"},
	}

	for _, c := range cases {
		r := rules[c.idx]
		if r.Name != c.name {
			t.Errorf("rule %d name: expected %q, got %q", c.idx, c.name, r.Name)
		}
		if r.MAC != c.mac {
			t.Errorf("rule %d mac: expected %q, got %q", c.idx, c.mac, r.MAC)
		}
		if r.Policy != c.policy {
			t.Errorf("rule %d policy: expected %q, got %q", c.idx, c.policy, r.Policy)
		}
		if r.WAN != c.wan {
			t.Errorf("rule %d wan: expected %q, got %q", c.idx, c.wan, r.WAN)
		}
	}
}

func TestNormalizeMAC(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"00:11:32:82:58:af", "00:11:32:82:58:AF"},
		{"00-11-32-82-58-af", "00:11:32:82:58:AF"},
		{"0:11:32:82:58:af", "00:11:32:82:58:AF"},
		{"invalid", ""},
	}
	for _, c := range cases {
		got := normalizeMAC(c.in)
		if got != c.out {
			t.Errorf("normalizeMAC(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestLoadDeviceRulesFromUCI(t *testing.T) {
	content := `
config mac-binding 'device1'
	option mac '00:11:32:82:58:af'
	option wan 'wan1'
	option name 'FnOS'
	option enabled '1'

config mac-binding 'device2'
	option mac 'bc:24:11:7b:6d:71'
	option wan 'wan2'
	option comment 'Tailscale'
`
	tmp := t.TempDir() + "/test.conf"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	rules := loadDeviceRulesFromUCI(tmp)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if !strings.EqualFold(rules[0].MAC, "00:11:32:82:58:AF") {
		t.Errorf("mac mismatch: %s", rules[0].MAC)
	}
	if rules[0].WAN != "wan1" {
		t.Errorf("wan mismatch: %s", rules[0].WAN)
	}
	if rules[0].Name != "FnOS" {
		t.Errorf("name mismatch: %s", rules[0].Name)
	}
}

func TestDeviceRuleDisplayPolicy(t *testing.T) {
	if got := DeviceRuleDisplayPolicy("wan1_only", "wan1"); got != "WAN1 独占" {
		t.Errorf("unexpected display policy: %s", got)
	}
	if got := DeviceRuleDisplayPolicy("wan2_priority", "wan2"); got != "WAN2 优先" {
		t.Errorf("unexpected display policy: %s", got)
	}
}
