# wan-manager 设计文档：分流引擎与更新子系统

> 状态：设计稿（2026-07-18）
> 范围：多 WAN 路由的「流 → 出口 WAN」决策模型，以及让系统可持续运行的三路更新机制。

---

## 0. 背景与方向修正

早期方案以「目的 DPI（端口 + 域名 → 应用）」为核心，对**网站类**（B站 / Netflix，域名稳定可枚举）有效。但在真实场景复盘后发现，该模型对**游戏类流量先天失效**：

- 游戏对战流量多为**直连 IP 的 UDP**，非 TLS、无 SNI、非 HTTP；
- 服务器 IP **未知且随大区 / 场次变化**，无法枚举成清单；
- 即便引入真 nDPI（L7 载荷），也只认内置签名的热门 PC 游戏，**认不出腾讯系私有协议（如王者荣耀）**；且 nDPI 给的是「类名」而非「目的 IP」，**对选路毫无帮助**；技术上还需在路由器挂 pcap / NF_QUEUE 抓包（CGo 交叉编译，重）。

**结论**：我们能确定的是「哪台设备」在玩，而不是「服务器在哪」。路由的基本单位应从「目的 → 应用」改为「源 / 目的 → 出口 WAN」的**分层匹配**。

---

## 1. 总体架构：分层匹配器

每个出向流按优先级匹配，命中即定 WAN：

```
流进入
  │
  ├─[1] 游戏 IP 库 (SSTap-Rule)    ← 游戏主用，社区替我们枚举服务器
  ├─[2] 源设备路由 (src IP)          ← 粗粒度兜底（整台手机 → WANx）
  ├─[3] 域名 DPI (已有 system.go)    ← 网站类（B站 / Netflix 走域名）
  └─[4] (未来) GeoIP / ASN           ← 按国家 / 地区
  │
  ▼
分配 WAN 出口（ip rule → 路由表 → WAN）
```

> 「王者这次走 WAN1、下次走 WAN2」= 翻转该游戏（或该设备）的出口开关。

---

## 2. 游戏 IP 库（核心，第 1 层）

### 2.1 数据源
`FQrabbit/SSTap-Rule`（GitHub，6.6k★，持续更新）：社区维护的「游戏 = 服务器 IP 段 + 域名」清单，原本给 SSTap 做「游戏走哪条代理」。多 WAN 场景只是把「代理节点」换成「某个 WAN 出口」——同一套规则，同一套思路。

### 2.2 格式（Phase 0 已实测验证）
每个 `.rules` 文件 = 一个游戏。实测 `FQrabbit/SSTap-Rule` 的真实格式**不是**早期假设的 `ip=/domain=/port=` INI 风格，而是：

```
#<标题>,<中文显示>,<6 个标志位>,By-<来源>      ← 第 1 行 # 头注释
18.172.52.0/24                                  ← 其余每行一个 IPv4 CIDR（/32 /24 /16 /4 均可）
13.225.183.0/24
...
```
- 第 1 行 `#` 头：逗号分隔，字段 = 标题 / 中文显示名 / 6 个标志位 / `By-来源`。
- 体部：每行一个 CIDR；空行与 `#` 注释行忽略；当前主流规则库**只有 CIDR，没有 `domain=`/`port=`**（解析器仍向前兼容这两种写法）。
- 解析器已落地：`internal/rules`（`ParseRuleFile` / `ParseDir`），含真实样本测试（`testdata/`）。

### 2.3 集成方式（已实现，2026-07-18）
```
解析 <游戏key>.rules  (rules.ParseRuleFile)
  → RuleFile.CIDRs 全部灌入 ipset  game_<清洗后key>   (hash:net)
  → iptables mangle WAN_MANAGER: -m set --match-set game_<key> dst -j MARK --set-mark <table_wanX>
  → ip rule: fwmark <table_wanX> table <table_wanX>  → 查表走指定 WAN
```
- `<key>` = 文件名去 `.rules` 后缀（如 `GenshinImpact.exe_SSTAP`）；ipset 名经 `sanitizeName` 清洗（小写、非字母数字下划线截断 20）。
- 仅 `enabled=true` 且 `wan` 命中已知 WAN 的游戏才建 ipset + 加规则。
- **目录**：`GameRulesDir`（`routing.toml` 的 `game_rules_dir`，默认 `rules/game`），由 `main` 解析为相对二进制 exe 的绝对路径后 `Manager.SetGameRulesDir` 注入。
- **已合并进「分流规则」**：游戏不再是独立 Tab/API，而是 `Rule` 的一种类型（`Type="game"`，字段 `Game`=`.rules` 文件名去后缀）。分配/启用/删除统一走 `GET/POST/PUT/DELETE /api/v1/routing`，与自定义/运营商/应用规则并排出现在同一个「分流规则」列表里。单独的只读接口 `GET /api/v1/game-library` 仅用于弹窗下拉选择可用的 `.rules` 文件。
- **端到端已验证**：起服务 → `GET /api/v1/game-library` 列出 4 个样本（标题/CIDR 数正确）→ `POST /api/v1/routing`（type=game）→ `GET /api/v1/routing` 的 `config.rules` 含该游戏规则 → `DELETE` 移除生效（ASCII 名验证通过；中文名删除受测环境终端 UTF-8 编码影响，属既有通病，非合并引入）。

### 2.4 更新与注意
- **新鲜度**：游戏 IP 会变、库会更新 → 定期 re-pull + reparse + reload。
- **覆盖度**：见 §2.5（Phase 0 实测：原神 ✅ 在库；王者荣耀 / 燕云十六声 ❌ 均不在库）。
- **CDN 共享 IP**：极少，可忽略。
- **重要修正（WebSearch 复核）**：王者 / 燕云**不仅不在库，且本质上无法用任何 IP-CIDR 方案路由**——详见 §2.5。手动抓 IP 是最差路，正解是源设备路由。
- **规模**：成千 CIDR，`ipset hash:net` 轻松扛。
- 纯 IP CIDR 清单，无版权 / ToS 问题。

### 2.6 实现落点（2026-07-18，2026-07-18 晚合并进分流规则）
- **配置**：`internal/routing/config.go` 的 `Rule` 新增 `Game string`（`toml:"game"`/`json:"game"`，= `.rules` 文件名去后缀）；`RoutingConfig` 保留 `GameRulesDir`；删除原独立的 `GameRouting` 结构与 `Games` 字段。`routing.toml.example` 的 `[[rules]]` 增加 `type="game"` + `game=` 示例。
- **加载与执行**：`internal/routing/manager.go`
  - `readGameCIDRs(name)` 读取 `<dir>/<name>.rules` 并 `rules.ParseRuleFile` 校验；
  - `createIPSets` / `setupIPTables` / `cleanupIPSets` 的 `Rules` 循环并入 `type="game"` 分支（建 `game_<key>`（hash:net）、灌 CIDR、`dst` 命中 → MARK、`cleanup` 销毁）；`gameIPSetName(rule)` 统一计算 ipset 名；
  - `GetConfigCopy` 深拷贝 `Rules`（含 `Game` 字段）；`Set/GetGameRulesDir` 存取目录。
- **API**：`internal/api/api.go`
  - `handleRouting` 的 add/put 支持 `type=game`：从 raw map 或结构体读取 `game` 字段，`validateGameRule` 校验 `.rules` 文件存在（否则 400）；分配/启用/删除统一走 `GET/POST/PUT/DELETE /api/v1/routing`。
  - 新增只读 `GET /api/v1/game-library`：列举目录下 `.rules`（标题/中文名/来源/CIDR 数/警告），供弹窗下拉；删除原独立的 `/api/v1/games` 与 `GameInfo`。
- **前端**：`internal/web/index.html` 删除独立的「游戏分流」Tab/页面/JS；规则弹窗类型下拉增加「游戏 IP 库」，选中后显示 `.rules` 文件下拉（`loadGameLibrary` 喂数据）；`分流规则` 列表的 IP 规则表内联渲染游戏徽标与来源；`submitRule/editRule/onRuleTypeChange` 适配 `game` 字段。
- **种子库**：`rules/game/` 随仓库与 release 分发 4 个样本（`GenshinImpact.exe_SSTAP` / `Steam` / `Valorant` / `PlayerUnknown's-Battlegrounds-update`）+ `README.md`；`release.yml` 打包 `rules/`，`install.sh` 拷到 `<INSTALL_DIR>/rules/game/`。

### 2.5 覆盖度现状（Phase 0 实测，2026-07-18）
- **原神** ✅ 在库：`GenshinImpact.exe_SSTAP.rules`（9 条 CIDR，来源 `By-ip_crawl_tool`）。
- **王者荣耀** ❌ **不在库**：全量 191 条规则无 Honor/King/KOG/Arena of Valor/Glory 任何匹配；腾讯系私有协议手游普遍缺失。
- **燕云十六声** ❌ **不在库**：无 Winds/Meet/Yanyun 匹配；仅有荒野行动、天涯明月刀等近似。
- **结论（更新）**：该库对 **PC / 外服游戏覆盖好**，对**腾讯系手游与较新国产游戏普遍缺**。WebSearch 进一步证实：这两款不仅不在库，**本质上无法用任何 IP-CIDR 方案路由**：
  - **王者荣耀 = 腾讯系**：后端大厅 / PVP / Proxy / 房间架构，**对战 PVP 服务器动态分配、用完回收**，IP 每次不固定、不可枚举；腾讯 AS132203 有 **200 万+ IPv4**（43.x / 1.x 大段，含微信 QQ 腾讯云），不能整段路由。
  - **燕云十六声 = 网易系**（包名 `com.netease.yyslscn`）：国服跑网易内网集群，IP 段无法干净切出；国际服（WWM）有公开阿里云 OSS 端点 IP 但国服无。
  - **推论**：两款都活在「超大 + 与无关服务共享」的厂商 IP 段里，王者的对战 IP 还动态变化 → **手动抓 IP 是最差路**（只抓到当次会话少量 IP、IP 一变即废、维护成本极高）。
  - **正解（备用）**：走 **§3 源设备路由**——不知道服务器 IP 也能 100% 覆盖，且服务器怎么变都不影响。但用户确认**小米路由本身已带此功能**，故本项目**不做源设备路由**（见 §3 注记）。SSTap-Rule 库保留给**原神等在库**的 PC 游戏作补充（**已实现**，见 §2.3 / §2.6）。

---

## 3. 源设备路由（已弃用，按用户决策）

> **决策（2026-07-18）**：用户指出**小米路由自带源设备路由功能**，故本项目**不做此层**。游戏类流量改由 **§2 游戏 IP 库（在库游戏）** 覆盖；腾讯/网易系手游（王者、燕云）既不在库、又无法用 IP 方案，由用户的小米路由做源设备级兜底。

按 `src IP` 打 mark → `ip rule` → 对应 WAN 表。对游戏是「不求知道服务器也能路由」的最简解；也对「整台设备走某 WAN」类需求直接可用。**（本项目不实现）**

---

## 3.1 运营商分流（ISP，已实现 2026-07-18）

按「目的 IP 所属运营商」选路：电信目的 → 电信 WAN、联通 → 联通 WAN，避免跨网绕路高延迟。这是多 WAN 最实用场景之一，且 IP 段**公开可枚举、相对稳定**（与游戏库相反），不存在「抓不到 IP」的问题。

### 3.1.1 数据源（已接入）
`metowolf/iplist`（GitHub，持续更新）的运营商分类：
- 电信 `chinatelecom.txt`、联通 `chinaunicom.txt`、移动 `chinamobile.txt`
- 经实测：电信 3082 / 移动 1493 / 联通 1914 条 CIDR，纯 IPv4 段，无域名/端口。

### 3.1.2 加载策略（`internal/rules/ispdata`）
- **远程优先**：启动时依次尝试 `metowolf.github.io` 镜像（CDN，每小时更新）→ `raw.githubusercontent` 回退，拉取最新 IP 段。
- **本地快照回退**：仓库内置 `internal/rules/ispdata/data/*.txt`，随二进制分发到 `<exe>/data/isp/`（release 打包与 `install.sh` 均已处理），离线 / 远程失败时使用。
- 解析：`ParseCIDRList` 逐行校验（`net.ParseCIDR`，裸 IP 自动补 `/32`，忽略 `#` / 空行，非法行进 warnings 不致命）。
- 与用户在 `[isp]` 内联的自定义段**去重合并**。

### 3.1.3 WAN 映射（不再写死）
- 早期 `isp_telecom→wan1 / isp_unicom→wan2 / isp_mobile→wan1` 写死，已废除。
- 改为**启动时识别一次**：`main` 用 `internal/isp` 检测器（`curl --interface <wan>` 查 ipip.net）判定每个 WAN 的运营商，建立 `运营商 → WAN 名称` 映射，经 `Manager.SetISPOperatorMap` 注入。
- 支持 `wan_mapping` 手动覆盖（如 `wan_mapping = { wan1 = "telecom" }`）；`auto_detect=false` 时完全依赖手动。
- 同一运营商命中多个 WAN 取首个并告警；未识别到对应 WAN 的运营商，跳过该运营商分流（不报错）。

### 3.1.4 执行（`manager.go`）
`createIPSets` 建 `isp_telecom/isp_unicom/isp_mobile`（hash:net）并灌入 IP 段；`setupIPTables` 仅在 `enabled=true` 时，按 `运营商→WAN` 映射为 `isp_*` 集合添加 `dst` 匹配 + MARK。

---

## 4. 三路更新子系统

**核心原则**：用户配置与可更新产物**物理隔离**，更新永不冲掉 `config.toml`。

### 4.1 目录布局
```
<runtime>/
  wan-manager            # 主程序（可更新）
  config.toml           # 用户配置（不更新，隔离）
  scripts/              # 脚本（可更新）
  rules/                # 规则库（可更新）
    sources.json        # 上游来源 + 锁定版本 / commit
    game/               # SSTap-Rule 的 .rules（或精选子集）
      *.rules
    cache/              # 解析后 ipset 列表 / JSON
  data/                # 运行态（flows 等）
```

### 4.2 三路职责
| 路 | 来源 | 机制 | 风险 | 触发 |
|---|---|---|---|---|
| 主程序 | 本项目 release（`wan-manager-linux-arm64.tar.gz`） | 版本注入 → 下载匹配 arch → checksum → 备份 + 原子替换 → 优雅重启 → 失败回滚 | 高（失联风险） | 手动 |
| 脚本 | `scripts/*.sh` | 拉最新覆盖 | 低 | 手动 |
| 规则库 | `FQrabbit/SSTap-Rule` | re-pull → reparse → reload ipset | 低 | 手动 + 定时(周) |

### 4.3 实现要点
- **版本注入**：补 `-ldflags -X main.Version=...`（此前 TODO），否则无法自判版本。
- **Updater 包** `internal/updater`：`Check() / Download() / Apply() / Rollback()`，统一「带超时下载 + 校验 + 原子替换」，暴露 `/api/v1/update/...`。
- **二进制回滚**：启动新进程失败自动还原旧档，避免路由器失联。
- **arch 感知**：下载须匹配运行架构（当前产物为 `linux-arm64`）。

---

## 5. 现有代码重新定位
- `internal/dpi/signatures.go` + `system.go`：从「主识别」降级为「网站层（第 3 层）」，不废弃。
- `manager.runCmd`（带超时）：复用为外部命令执行基础。
- `main.server.Shutdown`：复用于优雅重启。
- `--force-defaults` 等保留。

---

## 6. 范围之外（未来）
- 真 nDPI（L7 / pcap / NF_QUEUE / CGo）：已论证对游戏选路无实质帮助，暂不做。
- 行为学习：设备突发大量 UDP → 未知 IP 自动标「疑似游戏」做展示 / QoS（出口仍由设备 / 游戏层定）。
- GeoIP / ASN 地区路由。

---

## 7. 待确认 / TODO
- [x] ~~拉取并确认 王者 / 燕云 的 `.rules` 真实格式与覆盖度（Phase 0）~~ → **已完成**：真实格式 = 纯 CIDR + `#` 头（见 §2.2）；覆盖度：原神 ✅、王者荣耀 / 燕云十六声 ❌ 均不在库（见 §2.5）。
- [x] ~~运营商分流：动态 WAN↔ISP 映射（启动识别一次，不再写死）+ 自动加载 IP 段（metowolf/iplist，本地快照回退）~~ → **已完成**（2026-07-18，见 §3.1）。
- [x] ~~游戏 IP 库分流：解析 `.rules` → ipset → `ip rule` → 指定 WAN + UI~~ → **已完成**（2026-07-18，见 §2.3 / §2.6）。
- [x] ~~版本注入 `main.Version`~~ → release.yml 已用 `-X main.version=...` 注入。
- [ ] 源设备路由（§3）：**按用户决策弃用**——小米路由已自带。
- [ ] 确认路由器目标架构（arm64 / armv7 / x86_64 / mips）→ 影响二进制更新源。
- [ ] 规则库：全量 vs 用户精选游戏（ipset 规模与更新体积权衡）。
- [ ] 定时更新在 OpenWrt 上的实现（cron / 内置 ticker）。

---

## 8. 建议实施阶段
- **Phase 0** ✅：拉取真实 `.rules` 验证格式（纯 CIDR + `#` 头）与覆盖度（原神在库、王者/燕云不在），并落地 `internal/rules` 解析器 + 测试。
- **Phase 1** ✅：游戏 IP 库分流（`rules/game/` 目录 + `rules` 解析器 + `manager` ipset/iptables + `/api/v1/games` + 前端「游戏分流」页 + 种子库）。2026-07-18 完成。
- ~~**Phase 2：源设备路由（src IP → WAN）**~~ → **已弃用**：用户确认小米路由自带此功能。
- **Phase 3**：三路更新子系统（先规则库，后脚本，二进制最后且带回滚）。
- **Phase 4（未来）**：GeoIP / 行为学习。
