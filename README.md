<p align="center">
  <img src="docs/assets/hearth-banner.svg" width="100%" alt="Hearth — A quiet home for your game servers." />
</p>

<p align="center">
  面向小型自托管服务器的轻量游戏服控制台。<br />
  让朋友只用一个网址和密码，就能查看状态、更新版本或安全重启服务器。
</p>

<p align="center">
  <code>Go</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>Vue 3</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>Windows Server</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>Palworld 1.0</code>
</p>

---

![Hearth 控制台](docs/assets/dashboard.png)

## Hearth 是什么

Hearth 把前端和 API 合并为一个 Go 二进制，直接管理现有的 SteamCMD 游戏安装，
不要求 Docker，也不迁移存档。它目前专注于 Windows 上的幻兽帕鲁服务器；饥荒联机版
保留为后续适配器。

适合这样的场景：

- 只有几位固定朋友，不需要复杂的托管平台。
- 游戏已经安装并拥有现有存档。
- 希望朋友能自行更新、重启或备份，不再等待服主远程操作。
- 需要清楚的权限边界，但不想引入数据库和账号系统。

## 能力

| 范围 | 当前实现 |
| --- | --- |
| 游戏生命周期 | 启动、安全停止、重启、SteamCMD 更新和 ZIP 备份 |
| 帕鲁配置 | 分源读取和增量保存 `WorldOption.sav` / `PalWorldSettings.ini`，显示重复项冲突 |
| 状态监控 | CPU、内存、磁盘、进程、版本、存档 ID、在线玩家和运行时间 |
| 访问控制 | 一个管理员密码、最多 20 个无用户名成员密码、权限模板与后端能力校验 |
| 审计 | 登录 IP、凭据编号、成功状态和时间；不记录密码明文 |
| 部署 | 单文件 Windows 程序、计划任务自启动、版本化安装包 |

### 权限模型

| 操作 | 管理员 | 成员密码 |
| --- | :---: | :---: |
| 查看状态 | ✓ | 始终允许 |
| 启动、停止、重启 | ✓ | 可勾选 |
| 更新服务端 | ✓ | 可勾选 |
| 创建存档备份 | ✓ | 可勾选 |
| 修改帕鲁配置 | ✓ | 可勾选 |
| 管理成员密码 | ✓ | — |
| 查看任务日志与登录审计 | ✓ | — |

新成员默认只读。管理员可选“只读、日常管理、服主管理”模板，再调整具体勾选项。
成员密码使用随机盐和 PBKDF2-SHA256 摘要保存；前端按权限隐藏或禁用入口，后端仍会
独立校验每次请求。删除成员或修改其密码、权限时，该成员现有会话会立即失效。

## Windows 快速安装

要求：

- Windows Server 2022 或兼容版本
- 已安装 SteamCMD 和 Palworld Dedicated Server
- 管理员 PowerShell

构建安装包：

```bash
make windows-package
```

解压 `hearth-windows-amd64-v0.7.0.zip`，确保这些文件位于同一目录：

```text
hearth.exe
install-windows.ps1
uninstall-windows.ps1
VERSION
THIRD_PARTY_NOTICES.md
```

在管理员 PowerShell 中运行：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\install-windows.ps1
```

安装器输入的是 Hearth 管理员密码，与帕鲁的 `AdminPassword` 相互独立。安装器只安装
Hearth 和启动任务，不会停止、启动、更新或修改正在运行的 Palworld。

## 第一次接管 Palworld

1. 在现有方式下正常保存并关闭游戏。
2. 安装 Hearth，在 ECS 内打开 `http://127.0.0.1:8080`。
3. 从 Hearth 启动服务器；启动本身不依赖 REST API。
4. 如果需要玩家数据以及运行中的安全停止、重启、更新和备份，再进入 INI 配置来源，
   设置非空 `AdminPassword`、启用 `RESTAPIEnabled` 并确认 `RESTAPIPort=8212`。

Hearth 固定从 `127.0.0.1` 访问 Palworld REST API。REST 不可用时仍允许启动，
但依赖保存和优雅停服的操作会被锁定，且不会退化为强制结束游戏进程。

完整流程见 [Windows 帕鲁部署指南](docs/windows-palworld.md)。

## 公网访问

Hearth 默认只监听 `127.0.0.1:8080`，不会修改安全组或 Windows 防火墙。四人自用时，
推荐在 ECS 上通过 Tailscale Funnel 提供公网 HTTPS：

```powershell
tailscale funnel --bg 8080
tailscale funnel status
```

不要向公网开放 Hearth 的 8080 端口，也不要开放 Palworld REST API 的 8212 端口。
更完整的安全边界见 [架构决策](docs/architecture.md)。

## 本地开发

要求 Go 1.26+、Node.js 22+ 和 pnpm 10+。

```bash
pnpm --dir web install
make dev-api
make dev-web
```

演示环境地址为 `http://127.0.0.1:5173`，密码为 `admin`。

生产构建：

```bash
make build
```

产物位于 `bin/hearth`。本地演示也可以直接运行：

```bash
HEARTH_DEMO=true HEARTH_ADMIN_PASSWORD='replace-me' ./bin/hearth
```

## 项目边界

- 当前生产适配器只支持 Windows Palworld 1.0。
- 不执行来自前端的任意 Shell 文本。
- 同一游戏同时只运行一个变更任务。
- 不主动轮询 SteamCMD 或长期保存监控时间序列。
- 运行中的世界规则可以查看和编辑，但必须安全停服后才能写入。
- 不明确识别存档时拒绝猜测，不绑定固定世界 ID。

## 文档

- [Windows 帕鲁部署与升级](docs/windows-palworld.md)
- [架构与安全边界](docs/architecture.md)
- [第三方组件声明](THIRD_PARTY_NOTICES.md)
