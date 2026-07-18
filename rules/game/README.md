# 游戏规则目录 (rules/game)

本目录存放 **SSTap-Rule** 风格的游戏服务器 IP 规则文件（`.rules`），
程序启动时会扫描本目录，把每个启用游戏的 CIDR 灌入独立 `ipset`，
再按目的 IP 打 mark 选路 —— 实现「某游戏走指定 WAN」。

## 文件格式（已用真实库验证）

```
#<英文/区服名>,<中文显示名>,<6 个标志位>,By-<来源>
1.2.3.0/24
5.6.7.8/32
...
```

- 第 1 行为 `#` 头注释，用于展示游戏名与来源；
- 其余每行一个 **IPv4 CIDR**（裸 IP 自动补 `/32`），空行与 `#` 注释忽略；
- 向前兼容 `domain=` / `port=` 写法（当前主流库未使用）。

解析器见 `internal/rules/parser.go`，并配有 `internal/rules/parser_test.go` 端到端测试。

## 命名约定

文件名（去掉 `.rules` 后缀）即游戏主键，用于 ipset 命名与配置分配，例如：

- `GenshinImpact.exe_SSTAP.rules` → 主键 `GenshinImpact.exe_SSTAP`，ipset `game_genshinimpac`
- 前端会展示文件头里的**中文名**（如「原神-港澳台服」）。

## 如何新增游戏

1. 从 [FQrabbit/SSTap-Rule](https://github.com/FQrabbit/SSTap-Rule) 下载对应 `.rules` 放入本目录；
2. 在 Web 界面「游戏分流」页点「刷新」即可识别；
3. 在下拉框选择出口 WAN，点「应用」即生效（自动写入 `routing.toml` 的 `[[games]]`）。

## 已知覆盖度

- ✅ 在库：原神、Steam、无畏契约(Valorant)、PUBG 等大量 PC / 外服游戏；
- ❌ **不在库**：王者荣耀、燕云十六声等**腾讯 / 网易系手游**。
  这类游戏服务器 IP 多为动态分配、且与海量无关服务共享厂商 IP 段，
  IP 规则方案无法稳定覆盖，需改用**源设备路由**等方案。

> 本目录默认随二进制分发 4 个样本（原神 / Steam / 无畏契约 / PUBG），
> 仅作开箱演示，请按需补充你实际要路由的游戏。
