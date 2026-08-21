<p align="center">
  <strong>简体中文</strong>&nbsp;&nbsp;|&nbsp;&nbsp;<a href="windows-palworld.en.md">English</a>
</p>

# Hearth：Windows 帕鲁部署与安全接管

## 初始发现基线

2026-07-29 的只读发现报告曾确认：

- Windows Server 2022 Datacenter，4 个逻辑处理器、8 GB 内存
- SteamCMD 位于当前用户的 `Downloads\steamcmd`
- Palworld 使用 Steam App ID `2394010`
- `PalServer-Win64-Shipping-Cmd.exe` 正在运行
- UDP 8211 正在监听
- 没有发现 Windows 服务或计划任务
- 没有发现 TCP 8212 监听
- 当时存在两个 `steamcmd.exe` 进程

这是首次接管前的历史基线，不代表升级后的实时状态。最后两项在当时意味着 REST API
很可能尚未启用，而且再次更新前必须先退出原有 SteamCMD 会话。

## 推荐的首次部署方式

已有服务器有两种可行方式，空白服务器还有第三种：

1. 让面板直接接管正在运行的进程。可以识别状态，但首次停服依赖旧进程已经启用
   REST API。
2. 先按现有方式正常保存并关闭游戏，再安装面板、修改配置并由面板首次启动。
3. 只安装 Hearth；登录后由管理员在首次启动向导中选择空目录并确认安装 Palworld。

已有服务器采用第二种，流程更短，也不存在第一次接管时的停服不确定性。空白服务器采用
第三种。Hearth 启动时只做有界的只读探测，不会自动下载、接管或启动任何游戏。

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
和左下角节点卡片也会显示当前版本。升级后，没有权限字段的旧成员记录会按“只读”处理；
旧版“修改帕鲁配置”权限会自动收敛为“修改帕鲁玩法参数”，不会继续获得系统参数或
`WorldOption.sav` 访问。

安装器会：

1. 只读检查 SteamCMD、PalServer、当前配置和默认配置文件；缺失时仍允许安装 Hearth。
2. 把面板复制到 `C:\ProgramData\Hearth`。
3. 创建独立的面板管理员密码文件并收紧 ACL。
4. 配置成员密码摘要、登录/安全操作/参数审计、IP 规则和设备签名密钥；升级时保留已有内容。
5. 注册开机启动的 `Hearth` 计划任务。
6. 仅监听 `127.0.0.1:8080`。
7. 验证面板健康接口。
8. 将启动、停止和运行错误写入
   `C:\ProgramData\Hearth\panel.log`。

新配置默认启用两项长期运行保护：

- `backupRetentionDays: 30` 与 `backupMaxTotalGB: 20`：每次成功创建新备份后，先清理
  超过 30 天的 Hearth ZIP，再从最旧文件开始清理到总量不超过 20 GiB。新备份与非
  Hearth 命名文件不会被误删。
- `steamCmdNoProgressMinutes: 30`：SteamCMD 日志连续 30 分钟没有增长才判定卡死。
  SteamCMD 自身更新、Palworld 下载和文件校验只要仍输出日志，就会持续刷新计时。

安装器不会：

- 修改 `PalWorldSettings.ini`
- 修改任何世界目录中的 `WorldOption.sav`
- 停止或重启 Palworld
- 执行 SteamCMD
- 开放 Windows 防火墙端口
- 修改 RDP 或 SSH

## 首次游戏管理

- 没有可管理游戏时，管理员主页显示首次启动向导；成员只能看到“尚未配置”。
- 已经管理至少一个游戏后，后续探测、接管和安装位于独立的“系统设置 → 游戏管理”。
- “重新探测”只扫描受限的已知位置和管理员填写的目录，限制深度、目录数与候选数，只读取
  文件路径和元数据。
- 接管要求管理员选择稳定的探测候选并再次确认。Hearth 不会在接管时自动创建或覆盖
  `PalWorldSettings.ini`，也不会移动已有存档。
- 新安装只选择 SteamCMD 根目录；若其中没有 `steamcmd.exe`，该目录必须为空。Palworld
  固定安装到其标准 `steamapps\\common\\PalServer` 目录且该目录必须为空。管理员明确
  确认后才从 Valve 官方地址下载 SteamCMD 并安装 App `2394010`；完成后服务器保持停止。
- SteamCMD 先下载到同磁盘隔离暂存目录，校验 ZIP 路径与体积后再切换到目标目录。下载或
  解压中断不会把半成品目录当作有效安装。
- DST 支持识别并由管理员明确接管现有 Dedicated Server 与 cluster，也支持管理员确认的一键
  安装 App `343050` 并原子创建全新 Master/Caves cluster。Token 只写入 cluster_token.txt，
  不进入 Hearth 配置、接口响应或审计；模组和面板内备份恢复仍未开放。DST 详细流程见
  [Windows DST 部署指南](windows-dst.md)。

后台设置保存带修订号，拒绝覆盖并发修改；写入成功时保留 `config.json.previous`。运行期
安全参数在页面明确提示需要重启 Hearth，游戏进程不会因保存后台设置而重启。

## 更新 Hearth 面板

管理员从“系统设置 → 面板更新”手动检查并确认安装。公开仓库的 Release 可匿名读取；独立
更新器只替换 Hearth，在新版本健康检查失败时恢复旧程序，不停止 Palworld。
完整边界和人工恢复方式见 [面板安全更新指南](panel-update.md)。

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

面板管理员密码和新建/修改的成员密码最低为 10 个字符；建议实际使用 14 个字符以上的
唯一密码或长短语。

- 不设置用户名，每个密码自动分配一个 `M-...` 编号。
- 新成员默认只能查看状态；管理员可选择“只读、日常管理、服主管理”模板。
- 可分别勾选启动/停止/重启、更新服务端、创建备份和修改帕鲁玩法参数。
- 玩法参数只包含后端白名单中的日常规则；密码、REST/RCON、跨平台、模组、性能与磁盘
  参数、未知高级参数和 `WorldOption.sav` 始终仅管理员可操作。
- 任务日志、成员密码管理、登录与攻击、安全操作和参数审计始终仅管理员可见。
- 修改成员密码或权限后，该成员已有会话会立即退出。密码登录后的活动会话可在同一 Hearth
  进程内跨页面刷新恢复，最长 12 小时；Hearth 重启后仍需重新登录。
- 登录审计显示来源 IP、管理员/成员凭据编号、结果、疑似攻击等级与时间，不保存输入密码。
- 登录记录的三点菜单可把该次登录来源 IP 快速加入黑名单 24 小时或白名单 7 天；完整规则在
  “IP 黑白名单”页管理。黑名单在密码计算前拒绝，白名单仍然要求正确密码。
- 成员凭据、IP 规则、游戏接管/安装任务启动和后台设置保存进入独立“安全操作审计”，
  分别显示操作者来源 IP 与操作目标；不会显示为一次登录，也不会记录密码或密码摘要。
- 登录成功后浏览器会保存签名的可信设备 Cookie，但它只获得独立限流通道，不能免密
  登录、恢复活动会话或访问已认证接口。

相关文件都位于 `C:\ProgramData\Hearth`，并继承只允许 `SYSTEM` 和
Administrators 访问的 ACL：

```text
admin-password.txt
member-credentials.json
login-audit.jsonl
operation-audit.jsonl
config-audit.jsonl
ip-rules.json
device-cookie.key
```

`device-cookie.key` 用于签名可信设备标识，丢失只会让已有设备重新建立信任，不会丢失
成员凭据；不得把它复制到公开目录。`ip-rules.json` 保存管理员配置的精确 IP 规则，
命中次数是本次 Hearth 进程的运行时观测值，不会在攻击期间逐次写盘。

每次成功保存 `PalWorldSettings.ini`，服务端会记录操作者、来源 IP、保存时间、配置版本
和实际发生的参数变化。敏感值只显示“已修改”。管理员可在“访问权限 → 参数审计”查看
最近 1000 条；JSONL 文件达到 5 MiB 后轮转为 `.1` 并保留一代。当前不对
`WorldOption.sav` 做逐参数审计。登录审计文件采用相同的 5 MiB 单代轮转策略；安全操作
审计另存为 `operation-audit.jsonl`，保留最近 1000 条并独立轮转，避免登录攻击记录挤掉
管理员变更证据。

### 反向代理与真实 IP

安装器默认配置：

```json
"trustedProxyCidrs": ["127.0.0.0/8", "::1/128"]
```

因此只有与 Hearth 同机、从回环地址连接的反向代理可以提供
`X-Forwarded-For` / `X-Forwarded-Proto`。代理在另一台机器时，只加入那台代理的固定
地址或最小网段，并重启 Hearth；不要信任公网全网段。Hearth 从转发链右侧开始剥离可信
代理，格式异常、头部超过 1 KiB 或超过 8 跳时直接使用 TCP 对端地址。

Palworld 官方 1.0 REST API 不是启动游戏服务端进程的前提；它用于玩家数据、保存和优雅停服。
如需玩家数据、运行中的安全停止/重启、更新和备份，INI 配置至少需要：

```text
AdminPassword="使用一个非空管理密码"
RESTAPIEnabled=True
RESTAPIPort=8212
```

REST API 不应开放到公网。面板固定从 `127.0.0.1` 访问。

服务器详情页通过 `/v1/api/players` 展示在线游戏昵称，但面板 API 只保留清理后的显示名，
不会向浏览器返回 `accountName`、`playerId` 或 `userId`。在线人数以玩家列表长度为准，
玩家上限优先使用 `/v1/api/metrics` 的 `maxplayernum`。如果 REST 暂不可用且当前存档存在
`WorldOption.sav`，面板显示“上限未知”，不会回退到可能已被接管的 INI 人数值。

REST API 未启用或暂时不可用时，面板仍允许停止和重启，但会明确提示最近未自动保存的
进度可能丢失。确认后，任务先尝试 REST 安全关闭；只有安全关闭失败时，才终止任务创建时
识别到的同一个 Palworld PID。PID 已变化时任务会拒绝终止新进程。运行中的更新和备份不会
使用该回退路径，仍然要求 REST API 成功保存世界。

安装完成后：

1. 在 ECS 浏览器打开 `http://127.0.0.1:8080`。
2. 返回服务器页面即可点击“启动服务器”，启动本身不要求 REST API。
3. 如需完整管理能力，进入“帕鲁配置”，切换到 `PalWorldSettings.ini` 来源。
4. 设置非空 `AdminPassword`，把 `RESTAPIEnabled` 改为 `True`。
5. 确认 `RESTAPIPort` 为 `8212`，保存 INI 配置。

配置页把 `WorldOption.sav` 与 `PalWorldSettings.ini` 作为两个独立来源读取和保存。
管理员可以访问两个来源及全部参数；获授权成员只会看到 INI 白名单中的玩法参数，响应中
不包含原始 INI。即使绕过前端直接调用 API，系统或高风险参数也会被后端拒绝。
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
语义往返校验，其中浮点数按数值比较，`5` 与 `5.0` 视为等价。写入前面板创建完整 ZIP
备份；服务器运行期间允许查看和编辑，但必须
安全停止后才能保存。

启动任务只等待游戏进程出现，不要求 REST API。需要完整管理能力时，可以在 PowerShell
进一步确认 REST API：

```powershell
Test-NetConnection 127.0.0.1 -Port 8212
```

成功后，后续停止、重启、备份和更新都由面板执行。

如果选择保留旧进程继续运行，面板仍能只读显示状态；但在 REST API 验证成功前，
运行中的更新和备份会被拒绝。停止和重启仍会先尝试安全停服；只有用户在确认框中
明确接受存档风险后，REST 安全关闭失败时才会终止任务创建时识别到的原进程。
正常安全关闭会刷新在线人数：明确无人在线时通知等待缩短为 5 秒；有人在线或人数查询
失败时继续使用 `shutdownWaitSeconds`（默认 30 秒），详情页任务进度显示剩余时间。

## 检查是否有新版本

详情页“当前版本”会优先显示 Palworld REST API 返回的游戏版本；REST 不可用时显示
本机 `appmanifest_2394010.acf` 中的 Build ID。两者格式不同，所以面板不会直接把
游戏版本字符串与 Steam Build ID 混在一起比较。

Hearth 启动 30 秒后尝试首次检查，成功结果保留 6 小时；每 15 分钟只判断是否到期，失败
自动重试至少间隔 1 小时，不会持续调用 SteamCMD。拥有“更新服务端”权限的用户也可以
点击“检查新版本”。过程：

1. 读取本机 Steam manifest 的 `buildid` 作为结果缓存键，并读取已安装 depot 的 manifest ID。
2. 确认没有其他 SteamCMD 或 Hearth 任务正在执行。
3. 单独启动 SteamCMD 完成自身准备；其版本不会显示，也不参与服务端版本判断。
4. 再运行 SteamCMD 的 `app_info_print 2394010`，只刷新应用元数据，不下载或修改
   Palworld 服务端文件。
5. 读取 public 分支各 depot 的 manifest ID，只比较本机实际安装的 depot；App 顶层
   `buildid` 不直接参与更新结论。
6. 在当前版本旁只显示“服务端已是最新版”“Palworld 服务端有可用更新”或“服务端版本
   检查暂不可用”，不并排展示不同格式的 Build ID。

检查会显示任务阶段和进度。SteamCMD 准备阶段沿用 `steamCmdNoProgressMinutes`（默认
30 分钟）的无日志进展超时，只要自更新日志继续增长就不会被打断；Palworld 元数据查询
使用 2 分钟无进展超时。结果只保存在 Hearth 当前进程内；重启面板
或本机 Build ID 变化后会恢复为“尚未检查”。若 SteamCMD 查询在当前网络不可用，不影响
启动、停止、备份或正式更新。

Valve 将 [`app_info_print`](https://partner.steamgames.com/doc/sdk/uploading?l=english#DebuggingBuildIssues)
定义为显示当前 Steamworks 应用配置的调试命令；Hearth 只从其输出读取 public 分支
depot manifest。若 SteamCMD 正式更新明确输出 App `2394010` `already up to date`，Hearth
会显示“服务器已是最新版”并清除旧更新提示；其后的 SteamCMD 自身校验输出不作为
Palworld 下载进度。

## 任务日志

任务日志页不会在打开时扫描并读取多份历史文件。Hearth 在状态目录的 `task-history.json`
持久化任务与日志引用，保留最近 100 条且最长 30 天；记录滚动淘汰时同步删除关联任务日志。
升级前没有索引的孤立日志不会被猜测关联或自动删除。面板运行日志固定显示；进入页面时若有
运行中的启动、更新或版本检查任务，其关联日志自动成为可关闭标签。已完成任务的日志从左侧
对应操作记录打开，单次更新恢复服务器时产生的 SteamCMD 与游戏启动日志会归在同一操作下。
停止、备份和配置保存等没有独立控制台输出的操作不显示“查看日志”。日志正文每次最多读取
末尾 128 KiB，且接口只接受操作记录明确引用的日志文件。当前选中日志约每 0.75 秒刷新一次，
因此启动任务完成后仍可继续查看 Palworld 或 DST 分片的控制台输出。

## 1.4.1 开发中的官方模组管理

Palworld [官方服务器指南](https://docs.palworldgame.com/settings-and-operation/mod/) 规定 Windows
服务端从 PalServer 根目录的 `Mods\Workshop\<目录>\Info.json` 识别官方格式模组，并以
`Mods\PalModSettings.ini` 中重复出现的 `ActiveModList=<PackageName>` 判断启用项。模组需要完整
重启服务端后才由游戏自身部署。

1.4.1 提供管理员“帕鲁模组”页面及 API `GET /api/v1/games/palworld/mods`，有界扫描默认
Workshop 目录，展示 PackageName、版本、启用状态、`IsServer` 兼容性和可识别依赖。没有安装模组时
页面显示正常空状态。扫描不执行 JSON 中的规则、
不跟随符号链接、不读取超限文件，也不会复制、重命名或删除任何模组。检测到
`WorkshopRootDir` 时只提示尚未纳入当前清单，不会越过默认 PalServer 目录继续探测。

安装采用两段确认：管理员先输入 Workshop ID 或 Steam 详情链接，Hearth 通过固定的 Steam 官方
`GetPublishedFileDetails` 接口匿名读取名称、更新时间和文件大小；确认目标后，再上传从拥有
Palworld 的 Steam 客户端 Workshop 缓存取得的完整目录 ZIP。Steam 不向匿名 SteamCMD 提供 Palworld
Workshop 文件解密密钥，因此 Hearth 不会尝试匿名下载，也不保存 Steam 账号、密码或 Steam Guard。
详情查询遵循 Go 标准的 `HTTP_PROXY`、`HTTPS_PROXY` 和 `NO_PROXY` 环境变量；Windows 用户代理不会
被后台计划任务可靠继承。仅在服务器无法直连 Steam API 时，才应给 Hearth 进程显式配置代理并
重启任务，不需要在面板保存代理凭据。

上传和安装边界如下：

- Palworld 必须已停止；Hearth 不会为了安装模组自动停服或在完成后自动启动。
- ZIP 最大 256 MiB，解压后最大 1 GiB、最多 8192 项；拒绝路径穿越、符号链接、特殊文件和大小写
  重复路径。
- ZIP 根目录或唯一的直接子目录必须包含 `Info.json`。数字目录名若存在，必须与已确认的 Workshop
  ID 一致；`PackageName` 不能与现有模组重复，并且 `InstallRules` 必须明确包含 `IsServer=true`。
- 完整校验在 `Mods/.hearth-staging` 中进行，通过后单次重命名为
  `Mods/Workshop/<WorkshopID>`；失败只清理本次暂存或新建目录，不覆盖现有目录。
- 安装成功后写入 `Mods/HearthManagedMods.json` 记录 Hearth 所有权，但不修改
  `PalModSettings.ini`，所以新模组保持未启用。

接管现有目录、启用、停用、移除和依赖自动安装仍需补齐配置备份、计划预览、重启验证及原文件
回退后再开放。上传包无法通过 Steam API 加密证明一定对应所填 ID，管理员仍应在 Steam 页面和
ZIP 内容之间自行核对；Hearth 会独立校验包格式和服务端兼容声明。

## 安全更新流程

面板的更新顺序固定为：

1. 验证本机 REST API 和管理员密码。
2. 调用官方 `/v1/api/save`。
3. 调用官方 `/v1/api/shutdown` 并等待进程退出。
4. 将存档和配置写入独立 ZIP。
5. 确认没有其他 SteamCMD 进程。
6. 执行固定命令，由 SteamCMD 使用自身的 `steamapps\common\PalServer` 标准目录：

   ```text
   steamcmd.exe +login anonymous +app_update 2394010 +quit
   ```

7. 等待 SteamCMD 明确输出 App ID `2394010` 更新成功。首次运行如果只完成了 SteamCMD
   自身更新并正常退出，自动重试一次；不会把仅更新 SteamCMD 误报为 Palworld 更新完成。
8. 重新启动 PalServer，检查进程恢复。

SteamCMD 自更新、下载和校验期间，只要日志仍在增长，就不会触发无进展超时。默认连续
30 分钟没有日志变化时，Hearth 使用 Windows 进程树终止方式停止本次 SteamCMD，并提示
管理员重试；下一次运行仍会由 SteamCMD 自己校验和修复未完成文件。任何安全前置条件
失败时，任务停止，不会强杀游戏进程。

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
