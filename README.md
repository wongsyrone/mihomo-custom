# mihomo-custom

自动跟踪 `MetaCubeX/mihomo` 上游 Release 的补丁构建仓库（Patch-CI）。采用 **Package Overlay（同包扩展文件） + 纯标准库 Go AST 语义补丁器** 架构，避免上游新增相邻字段、重排函数或调整行号导致传统 unified diff 失效，并通过 GitHub Actions 自动编译发布 Windows / Linux / macOS 主流架构内核（`windows-amd64`、`windows-amd64-compatible`、`windows-arm64`、`linux-amd64`、`linux-arm64`、`darwin-amd64`、`darwin-arm64`）。

## 架构说明

1. **`overlay/`（纯新增包文件，零上下文冲突）**
   - `overlay/sing-tun/`：`DefaultInterfaceMonitor` 的声明式主备选路算法（`monitor_priority.go`）及单测（`monitor_priority_test.go`）。
   - `overlay/mihomo/`：切网开关追踪与清理（`listener/sing_tun/reset.go`）、`default-interface-priority` 双向兼容测试（`config/tun_priority_test.go`）、各协议及传输层的非破坏性 `ResetSessions()` 实现与零网络单测。
2. **`tools/patcher/`（纯 Go 标准库 AST 语义补丁器，类似 Coccinelle）**
   - 直接按 Go 语法节点（`TypeSpec`、`InterfaceType`、`StructType`、`FuncDecl`、`CompositeLit` 等）精确定位并改写上游已有文件，自动维护 `import` 与注释映射，不依赖文本行号与前后 3 行上下文。
   - 若上游破坏性重构导致目标语法节点缺失，补丁器会显式报错退出，并由 CI 自动开 Issue 提醒，同时每次构建都会导出 `sing-tun-semantic.diff` 与 `mihomo-semantic.diff` 作为 Workflow Artifact 供审计。

## 核心增强功能

1. **声明式默认出口优先级（`tun.default-interface-priority`）**
   - 同步覆盖 Windows（`monitor_windows.go`）、Linux（`monitor_linux_default.go` / netlink `Priority`）与 macOS（`monitor_darwin.go` / BSD RIB + `RTF_IFSCOPE`）：`checkUpdate()` 优先按 `tun.default-interface-priority` 列表顺序选择已连接且具备默认路由的接口，不受自动跃点数、链路速率波动或路由表顺序影响；仅当列表内接口均不可用时回退到最小跃点（同跃点优先保持当前接口）。
   - **OS 非默认明细路由优先保留**：`cDialerInterfaceFinder` 先通过 `RouteInterface()` 按最长前缀匹配查询 OS 非默认路由表（如内网网段静态路由或 VPN / 虚拟网卡路由，排除 TUN 自身），命中时走对应网卡而非强制劫持到 `default-interface-priority` 主网卡。
2. **切网存量连接与全协议会话池非破坏性重置**
   - 修复 `autoCloseProxyAdapter` 包装层方法透传，在默认出口发生实际切换时（如 `Wi-Fi` 断开切换到 `Ethernet 2`），以 `500ms` 上限并发触发未绑定其他健康网卡的适配器重置并关闭 `statistic.DefaultManager` 存量连接；显式通过 `interface-name` 绑定在健康网卡（如 `pinned-direct` → `Ethernet 2`）以及通过健康非默认路由网卡出站的 `DIRECT` 连接在主备切换与回切时保持不断。
   - 覆盖全部有状态 / 连接池 / 长连接协议，清理旧套接字后仍保留下次拨号自动重建能力：`anytls`、`singmux`（及级联内层适配器）、`trojan`/`vless`/`vmess` 下的 `gun`（gRPC）/ `xhttp` / `mekya`、`hysteria2`（含 Realm 控制面）、`hysteria`、`tuic`（v4/v5）、`shadowquic`、`trusttunnel`、`ssh`、`snell`（复用池带代际校验防旧连接回池）、`shadowsocks+kcptun`、`sudoku`（mux 与 HTTPMask）、`mieru`、`masque`（L3/L4/H2）、`openvpn` 与 `wireguard`（触发 `BindUpdate()`）。
   - 自带内置网络漫游引擎的 mesh VPN 守护进程（`tailscale`、`easytier`、`zerotier`）保留其原生漫游机制。

## 官方版本双向兼容性

- **自定义配置 → 官方内核**：官方 Mihomo 通过 `yaml.Unmarshal` 解析 `config.yaml`（未启用严格未知字段校验），会静默忽略 `tun.default-interface-priority`。因此同一份 `config.yaml` 可直接在官方版与 `mihomo-custom` 之间切换使用，无需反复注释配置。
- **官方配置 → 自定义内核**：当配置省略或置空 `tun.default-interface-priority` 时，`PreferredInterfaces` 为空且 JSON/YAML 序列化通过 `omitempty` 省略该字段，默认路由选路完全回退至上游最小跃点逻辑（并保留同跃点防抖粘性）。

## 推送到 GitHub 并启用自动编译

```powershell
cd <repo-dir>
git remote add origin https://github.com/<your-username>/mihomo-custom.git
git push -u origin main
```

1. 在 GitHub 仓库页面创建同名仓库（或使用已有空仓库）并推送 `main`。
2. 确认仓库 `Settings → Actions → General → Workflow permissions` 允许 **Read and write permissions**（用于发布 Release 和在语义补丁失败时建 Issue）。
3. 进入 `Actions → Build Custom Mihomo → Run workflow` 即可立即触发一次编译发布；之后每天 UTC 03:17（北京时间 11:17）会自动检查上游最新 Release，有新版本则自动跑 `tools/patcher` 单测、打语义补丁、跑内核单测并发布。

## 启用方式

1. 从本仓库 GitHub Releases 下载对应平台的二进制包（如 Windows `mihomo-windows-amd64-<tag>.zip`、Linux `mihomo-linux-amd64-<tag>.gz`、macOS `mihomo-darwin-arm64-<tag>.gz`，并校验 `SHA256SUMS.txt`）替换并重启服务。
2. 在 `config.yaml` 的 `tun:` 段配置 `default-interface-priority`（按系统网卡名填写，如 Windows `Wi-Fi` / `Ethernet 2`、Linux `wlan0` / `eth0`、macOS `en0` / `en7`）并重载配置：

```yaml
tun:
  auto-detect-interface: true
  default-interface-priority:
    - Wi-Fi
    - Ethernet 2
```

3. Windows 全部网卡保持默认 `AutomaticMetric = Enabled`，无需再手工计算或修改跃点数。
