# mihomo-custom

自动跟踪 `MetaCubeX/mihomo` 上游 Release 的补丁构建仓库（Patch-CI），通过 GitHub Actions 编译发布带自定义补丁的 Windows amd64 内核。

## 包含补丁

1. **`patches/sing-tun/0001-default-interface-priority.patch`**
   - 为 `sing-tun` 的 `DefaultInterfaceMonitorOptions` 新增 `PreferredInterfaces []string`。
   - Windows `checkUpdate()` 优先按 `PreferredInterfaces` 顺序选择已连接且具备默认路由的接口，不再受接口自动跃点数或链路速率波动影响；仅当列表内接口均不可用时回退到最小跃点（同跃点优先保持当前接口）。
2. **`patches/mihomo/0001-tun-interface-priority-and-reset-conns.patch`**
   - 在 `tun:` 下新增 `default-interface-priority` 配置项（列表顺序即优先级）。
   - 修复 `autoCloseProxyAdapter` 包装层方法透传并在默认出口发生实际切换时（如 `Wi-Fi` 断开切换到 `Ethernet 2`），限期并发清理未显式绑定其他网卡的存量连接与 `anytls` 会话池，避免旧 socket 卡死超时；显式通过 `interface-name` 绑定在健康网卡（如 `pinned-direct` → `Ethernet 2`）的连接保持不断。
3. **`patches/mihomo/0002-outbound-reset-sessions.patch`**
   - 为其余有状态/连接池/长连接协议逐一实现非破坏性 `ResetSessions()`（只清理失效连接或重建复用池，不永久关闭适配器，下次请求自动按新出口重建）：`singmux`（及级联内层协议）、`trojan`/`vless`/`vmess` 下的 `gun`（gRPC）/ `xhttp` / `mekya`、`hysteria2`（含 Realm 控制面）、`hysteria`、`tuic`（v4/v5）、`shadowquic`、`trusttunnel`、`ssh`、`snell`（复用池带代际校验防旧连接回池）、`shadowsocks+kcptun`、`sudoku`（mux 与 HTTPMask）、`mieru`、`masque`（L3/L4/H2）、`openvpn` 与 `wireguard`（触发 `BindUpdate()`）。
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
2. 确认仓库 `Settings → Actions → General → Workflow permissions` 允许 **Read and write permissions**（用于发布 Release 和在补丁冲突时建 Issue）。
3. 进入 `Actions → Build Custom Mihomo → Run workflow` 即可立即触发一次编译发布；之后每天 UTC 03:17（北京时间 11:17）会自动检查上游最新 Release，有新版本则自动打补丁、跑单测并发布。

## 启用方式

1. 从本仓库 GitHub Releases 下载对应版本的 `mihomo-windows-amd64-<tag>.zip`（或配合本地升级脚本拉取并校验 `SHA256SUMS.txt`）替换并重启服务。
2. 在 `config.yaml` 的 `tun:` 段配置 `default-interface-priority` 并重载配置：

```yaml
tun:
  auto-detect-interface: true
  default-interface-priority:
    - Wi-Fi
    - Ethernet 2
```

3. Windows 全部网卡保持默认 `AutomaticMetric = Enabled`，无需再手工计算或修改跃点数。
