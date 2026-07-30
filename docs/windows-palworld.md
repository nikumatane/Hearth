# Hearth：Windows 帕鲁部署与安全接管

## 当前服务器状态

2026-07-29 的只读发现报告确认：

- Windows Server 2022 Datacenter，4 个逻辑处理器、8 GB 内存
- SteamCMD 位于当前用户的 `Downloads\steamcmd`
- Palworld 使用 Steam App ID `2394010`
- `PalServer-Win64-Shipping-Cmd.exe` 正在运行
- UDP 8211 正在监听
- 没有发现 Windows 服务或计划任务
- 没有发现 TCP 8212 监听
- 当时存在两个 `steamcmd.exe` 进程

最后两项意味着：REST API 很可能尚未启用，而且再次更新前必须先退出原有
SteamCMD 会话。

## 推荐的首次部署方式

有两种可行方式：

1. 让面板直接接管正在运行的进程。可以识别状态，但首次停服依赖旧进程已经启用
   REST API。
2. 先按现有方式正常保存并关闭游戏，再安装面板、修改配置并由面板首次启动。

采用第二种，流程更短，也不存在第一次接管时的停服不确定性。接管识别能力仍然
保留，用于异常恢复。

正式安装前：

1. 确认没有玩家在线。
2. 使用目前的正常方式保存世界并关闭 Palworld。
3. 如果 SteamCMD 窗口处于空闲交互状态，在窗口输入 `quit` 正常退出；如果正在
   更新则等待更新完成。
4. 在任务管理器确认 Palworld 和 SteamCMD 进程均已退出。

## 安装

将构建包解压到 Windows 临时目录，以管理员身份运行：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\install-windows.ps1
```

不要只复制旧目录里的 EXE；应完整解压同一版本的 ZIP，确保 `VERSION`、
`install-windows.ps1` 和 `hearth.exe` 来自同一个包。升级已有 Hearth 时
运行 `.\install-windows.ps1 -Force`。安装器会核对启动后健康接口的版本号，登录页
和左下角节点卡片也会显示当前版本。升级后，旧成员记录因为没有权限字段会按“只读”处理，
管理员登录后再为需要操作服务器的成员明确勾选权限。

安装器会：

1. 验证 SteamCMD、PalServer、当前配置和默认配置文件。
2. 把面板复制到 `C:\ProgramData\Hearth`。
3. 创建独立的面板管理员密码文件并收紧 ACL。
4. 配置成员密码摘要文件和登录审计文件；升级时保留已有内容。
5. 注册开机启动的 `Hearth` 计划任务。
6. 仅监听 `127.0.0.1:8080`。
7. 验证面板健康接口。
8. 将启动、停止和运行错误写入
   `C:\ProgramData\Hearth\panel.log`。

安装器不会：

- 修改 `PalWorldSettings.ini`
- 修改任何世界目录中的 `WorldOption.sav`
- 停止或重启 Palworld
- 执行 SteamCMD
- 开放 Windows 防火墙端口
- 修改 RDP 或 SSH

## 当前存档识别

面板不写死世界 ID。每次读取状态或配置时按以下顺序识别当前世界：

1. 读取 `Pal\Saved\Config\WindowsServer\GameUserSettings.ini` 中的
   `DedicatedServerName`，并确认对应目录包含 `Level.sav`。
2. 如果没有有效的明确配置，则扫描 `Pal\Saved\SaveGames\0`；只有一个包含
   `Level.sav` 的 32 位世界目录时使用它。
3. 如果发现多个候选世界且没有明确配置，面板拒绝猜测并显示错误。

识别出的世界 ID 和识别依据会显示在状态页版本号、端口信息下方。

## 第一次配置并启动

安装器提示输入的 `Panel administrator password` 是面板管理员密码，不是
Palworld 的 `AdminPassword`。管理员登录后可在“访问权限”添加成员密码：

- 不设置用户名，每个密码自动分配一个 `M-...` 编号。
- 新成员默认只能查看状态；管理员可选择“只读、日常管理、服主管理”模板。
- 可分别勾选启动/停止/重启、更新服务端、创建备份和修改帕鲁配置。
- 任务日志、成员密码管理和登录 IP 审计始终仅管理员可见。
- 修改成员密码或权限后，该成员已有会话会立即退出；每次打开或刷新页面仍需重新登录。
- 登录审计显示来源 IP、管理员/成员凭据编号、结果与时间，不保存输入密码。

相关文件都位于 `C:\ProgramData\Hearth`，并继承只允许 `SYSTEM` 和
Administrators 访问的 ACL：

```text
admin-password.txt
member-credentials.json
login-audit.jsonl
```

Palworld 官方 1.0 REST API 不是启动 PalServer.exe 的前提；它用于玩家数据、保存和优雅停服。
如需运行中的停止、重启、更新和备份，INI 配置至少需要：

```text
AdminPassword="使用一个非空管理密码"
RESTAPIEnabled=True
RESTAPIPort=8212
```

REST API 不应开放到公网。面板固定从 `127.0.0.1` 访问。

安装完成后：

1. 在 ECS 浏览器打开 `http://127.0.0.1:8080`。
2. 返回服务器页面即可点击“启动服务器”，启动本身不要求 REST API。
3. 如需完整管理能力，进入“帕鲁配置”，切换到 `PalWorldSettings.ini` 来源。
4. 设置非空 `AdminPassword`，把 `RESTAPIEnabled` 改为 `True`。
5. 确认 `RESTAPIPort` 为 `8212`，保存 INI 配置。

配置页把 `WorldOption.sav` 与 `PalWorldSettings.ini` 作为两个独立来源读取和保存。
`WorldOption.sav` 按以下概念分类展示已支持参数：

- 服务器与连接
- 时间、成长与生成
- 玩家与帕鲁
- 战斗、事件与死亡
- 资源、物品与建造
- 据点与公会
- 旅行与世界功能
- 性能与数量限制

重复参数会显示来源冲突，但面板不会自动决定优先级或跨文件同步。两种来源都只提交用户
明确修改的参数；密码未重新输入时永不写回。`WorldOption.sav` 写入前还会执行完整
语义往返校验。写入前面板创建完整 ZIP 备份；服务器运行期间允许查看和编辑，但必须
安全停止后才能保存。

启动任务只等待游戏进程出现，不要求 REST API。需要完整管理能力时，可以在 PowerShell
进一步确认 REST API：

```powershell
Test-NetConnection 127.0.0.1 -Port 8212
```

成功后，后续停止、重启、备份和更新都由面板执行。

如果选择保留旧进程继续运行，面板仍能只读显示状态；但在 REST API 验证成功前，
停止、重启和更新按钮会被后端拒绝。面板绝不会用强制结束进程替代安全停服。

## 安全更新流程

面板的更新顺序固定为：

1. 验证本机 REST API 和管理员密码。
2. 调用官方 `/v1/api/save`。
3. 调用官方 `/v1/api/shutdown` 并等待进程退出。
4. 将存档和配置写入独立 ZIP。
5. 确认没有其他 SteamCMD 进程。
6. 执行固定命令：

   ```text
   steamcmd.exe +force_install_dir <安装目录> +login anonymous +app_update 2394010 +quit
   ```

7. 重新启动 PalServer，检查进程恢复。

任何安全前置条件失败时，任务停止，不会强杀游戏进程。

## 回滚面板

以管理员身份运行：

```powershell
.\uninstall-windows.ps1
```

它只停止并删除面板计划任务，保留：

- `C:\ProgramData\Hearth` 中的配置
- Palworld 程序和配置
- 世界存档
- `panel-backups` 中的备份

因此回滚面板不会影响正在运行的游戏。

## 给朋友使用的公网 HTTPS

可选方案有两种：

1. Tailscale Funnel：ECS 上安装一次，朋友只使用 HTTPS 地址和面板密码；不需要
   域名，也不需要开放面板端口。
2. Cloudflare Tunnel：能力更完整，但需要 Cloudflare 账户、域名和额外的访问策略
   配置。

四人自用采用第一种。Windows Server 2022 安装并登录 Tailscale 后，在管理员
PowerShell 执行：

```powershell
tailscale funnel --bg 8080
tailscale funnel status
```

把状态输出中的 `https://...ts.net` 地址发给朋友。Funnel 会自动提供 HTTPS，
面板在 ECS 上仍只监听 `127.0.0.1:8080`。

必须同时做到：

- 从 ECS 安全组删除临时 TCP 8080 公网规则。
- Windows 防火墙不开放 8080。
- 不开放帕鲁 REST API 8212。
- 面板密码使用至少 16 位、且不与帕鲁管理密码相同的随机密码。

关闭入口：

```powershell
tailscale funnel reset
```

Funnel 当前为 beta。若它不可用，回滚只需执行上面的 reset；面板、帕鲁进程和存档
都不会受到影响，仍可在 RDP 内使用 `http://127.0.0.1:8080`。
