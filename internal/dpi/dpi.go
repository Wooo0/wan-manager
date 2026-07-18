package dpi

import (
	"sync"
	"time"
)

type FlowInfo struct {
	ID          uint64
	SrcIP       string
	DstIP       string
	SrcPort     uint16
	DstPort     uint16
	Protocol    string
	Application string
	Detected    bool
	DetectedAt  time.Time
	Packets     int
	Bytes       int
}

type Detector interface {
	Start() error
	Stop() error
	GetFlow(flowID uint64) (*FlowInfo, bool)
	GetAllFlows() []FlowInfo
	DetectPacket(packet []byte) (*FlowInfo, error)
	RegisterCallback(fn FlowCallback)
}

type FlowCallback func(flow *FlowInfo)

type BaseDetector struct {
	mu        sync.RWMutex
	flows     map[uint64]*FlowInfo
	callbacks []FlowCallback
	running   bool
	flowSeq   uint64
}

func NewBaseDetector() *BaseDetector {
	return &BaseDetector{
		flows:     make(map[uint64]*FlowInfo),
		callbacks: make([]FlowCallback, 0),
	}
}

func (d *BaseDetector) RegisterCallback(fn FlowCallback) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.callbacks = append(d.callbacks, fn)
}

func (d *BaseDetector) notifyCallbacks(flow *FlowInfo) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, cb := range d.callbacks {
		cb(flow)
	}
}

func (d *BaseDetector) GetFlow(flowID uint64) (*FlowInfo, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	flow, ok := d.flows[flowID]
	return flow, ok
}

func (d *BaseDetector) GetAllFlows() []FlowInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]FlowInfo, 0, len(d.flows))
	for _, flow := range d.flows {
		result = append(result, *flow)
	}
	return result
}

func (d *BaseDetector) nextFlowID() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.flowSeq++
	return d.flowSeq
}

type ApplicationInfo struct {
	Name        string
	Category    string
	Risk        int
	Description string
}

var AppCatalog = map[string]ApplicationInfo{
	"http":          {Name: "HTTP", Category: "Web", Risk: 1, Description: "超文本传输协议"},
	"https":         {Name: "HTTPS", Category: "Web", Risk: 0, Description: "安全超文本传输协议"},
	"dns":           {Name: "DNS", Category: "基础服务", Risk: 0, Description: "域名系统"},
	"ssh":           {Name: "SSH", Category: "远程管理", Risk: 2, Description: "安全外壳协议"},
	"ftp":           {Name: "FTP", Category: "文件传输", Risk: 2, Description: "文件传输协议"},
	"smtp":          {Name: "SMTP", Category: "邮件", Risk: 1, Description: "简单邮件传输协议"},
	"pop3":          {Name: "POP3", Category: "邮件", Risk: 1, Description: "邮局协议版本3"},
	"imap":          {Name: "IMAP", Category: "邮件", Risk: 1, Description: "互联网消息访问协议"},
	"telnet":        {Name: "Telnet", Category: "远程管理", Risk: 3, Description: "远程登录协议"},
	"rdp":           {Name: "RDP", Category: "远程管理", Risk: 2, Description: "远程桌面协议"},
	"vnc":           {Name: "VNC", Category: "远程管理", Risk: 2, Description: "虚拟网络计算"},
	"torrent":       {Name: "BitTorrent", Category: "P2P", Risk: 2, Description: "BT下载协议"},
	"edonkey":       {Name: "eDonkey", Category: "P2P", Risk: 2, Description: "电驴下载协议"},
	"thunder":       {Name: "迅雷", Category: "P2P", Risk: 2, Description: "迅雷下载"},
	"qq":            {Name: "QQ", Category: "即时通讯", Risk: 1, Description: "腾讯QQ"},
	"wechat":        {Name: "微信", Category: "即时通讯", Risk: 1, Description: "微信"},
	"whatsapp":      {Name: "WhatsApp", Category: "即时通讯", Risk: 1, Description: "WhatsApp"},
	"telegram":      {Name: "Telegram", Category: "即时通讯", Risk: 1, Description: "Telegram"},
	"skype":         {Name: "Skype", Category: "即时通讯", Risk: 1, Description: "Skype"},
	"douyin":        {Name: "抖音", Category: "视频", Risk: 2, Description: "抖音短视频"},
	"bilibili":      {Name: "哔哩哔哩", Category: "视频", Risk: 1, Description: "哔哩哔哩"},
	"netflix":       {Name: "Netflix", Category: "视频", Risk: 1, Description: "Netflix"},
	"youtube":       {Name: "YouTube", Category: "视频", Risk: 1, Description: "YouTube"},
	"tiktok":        {Name: "TikTok", Category: "视频", Risk: 1, Description: "TikTok"},
	"youku":         {Name: "优酷", Category: "视频", Risk: 1, Description: "优酷视频"},
	"iqiyi":         {Name: "爱奇艺", Category: "视频", Risk: 1, Description: "爱奇艺"},
	"tencent":       {Name: "腾讯视频", Category: "视频", Risk: 1, Description: "腾讯视频"},
	"steam":         {Name: "Steam", Category: "游戏", Risk: 1, Description: "Steam游戏平台"},
	"epic":          {Name: "Epic Games", Category: "游戏", Risk: 1, Description: "Epic游戏平台"},
	"lol":           {Name: "英雄联盟", Category: "游戏", Risk: 1, Description: "英雄联盟"},
	"valorant":      {Name: "无畏契约", Category: "游戏", Risk: 1, Description: "无畏契约"},
	"csgo":          {Name: "CS:GO", Category: "游戏", Risk: 1, Description: "反恐精英全球攻势"},
	"weibo":         {Name: "微博", Category: "社交", Risk: 1, Description: "新浪微博"},
	"zhihu":         {Name: "知乎", Category: "社交", Risk: 0, Description: "知乎"},
	"xiaohongshu":   {Name: "小红书", Category: "社交", Risk: 1, Description: "小红书"},
	"taobao":        {Name: "淘宝", Category: "购物", Risk: 1, Description: "淘宝网"},
	"jd":            {Name: "京东", Category: "购物", Risk: 1, Description: "京东"},
	"pdd":           {Name: "拼多多", Category: "购物", Risk: 1, Description: "拼多多"},
	"aliyun":        {Name: "阿里云", Category: "云服务", Risk: 0, Description: "阿里云"},
	"tencent_cloud": {Name: "腾讯云", Category: "云服务", Risk: 0, Description: "腾讯云"},
	"huaweicloud":   {Name: "华为云", Category: "云服务", Risk: 0, Description: "华为云"},
	"icmp":          {Name: "ICMP", Category: "网络协议", Risk: 0, Description: "互联网控制消息协议"},
	"igmp":          {Name: "IGMP", Category: "网络协议", Risk: 0, Description: "互联网组管理协议"},
	"tcp":           {Name: "TCP", Category: "网络协议", Risk: 0, Description: "传输控制协议"},
	"udp":           {Name: "UDP", Category: "网络协议", Risk: 0, Description: "用户数据报协议"},
}
