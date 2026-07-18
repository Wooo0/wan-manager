package dpi

import "strings"

// wellKnownPorts 知名端口 -> 应用标识（与 AppCatalog 对齐）。
// 用于无 DNS 关联时的端口级兜底识别。
var wellKnownPorts = map[uint16]string{
	53:    "dns",
	80:    "http",
	443:   "https",
	22:    "ssh",
	21:    "ftp",
	25:    "smtp",
	110:   "pop3",
	143:   "imap",
	23:    "telnet",
	3389:  "rdp",
	5900:  "vnc",
	8000:  "qq",
	27015: "steam",
	9000:  "thunder",
	6881:  "torrent",
	4662:  "edonkey",
	5222:  "wechat",
	5223:  "wechat",
}

// domainRules 域名关键字 -> 应用标识。
// 用于按 AdGuard 解析记录（DNS 域名）识别应用，可区分同端口不同站点。
var domainRules = []struct {
	contains string
	app      string
}{
	{"youtube", "youtube"},
	{"youtu", "youtube"},
	{"bilibili", "bilibili"},
	{"netflix", "netflix"},
	{"douyin", "douyin"},
	{"tiktok", "tiktok"},
	{"wechat", "wechat"},
	{"qq.com", "qq"},
	{"taobao", "taobao"},
	{"jd.com", "jd"},
	{"pinduoduo", "pdd"},
	{"xiaohongshu", "xiaohongshu"},
	{"weibo", "weibo"},
	{"zhihu", "zhihu"},
	{"steampowered", "steam"},
	{"steam", "steam"},
	{"steamcommunity", "steam"},
	{"aliyun", "aliyun"},
	{"myqcloud", "tencent_cloud"},
	{"tencentcloud", "tencent_cloud"},
	{"huaweicloud", "huaweicloud"},
	{"epicgames", "epic"},
	{"riotgames", "valorant"},
	{"whatsapp", "whatsapp"},
	{"telegram", "telegram"},
	{"skype", "skype"},
	{"thunder", "thunder"},
}

// genericApps 通用协议级标识：仅作展示，不应触发 ipset 分流回调，避免污染。
var genericApps = map[string]bool{
	"http": true, "https": true, "dns": true,
	"tcp": true, "udp": true, "icmp": true, "igmp": true,
}

func classifyByPort(dport uint16) string {
	if app, ok := wellKnownPorts[dport]; ok {
		return app
	}
	return ""
}

func classifyByDomain(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	for _, r := range domainRules {
		if strings.Contains(d, r.contains) {
			return r.app
		}
	}
	return ""
}

func isGenericApp(app string) bool {
	return genericApps[app]
}
