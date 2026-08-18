<p align="center">
  <strong>简体中文</strong>&nbsp;&nbsp;|&nbsp;&nbsp;<a href="windows-dst.en.md">English</a>
</p>

# Windows DST 部署与升级

本文说明如何让 Hearth 安装或接管饥荒联机版 Dedicated Server，并管理 Master/Caves 双分片。
Hearth 不会在启动、探测或面板升级时静默安装游戏；所有写入都需要管理员在“系统设置 →
游戏管理”明确确认。

## 两种入口

### 接管现有服务器

适用于已经有世界的服务器。先正常停止 Master/Caves，再点击“重新探测”，核对 Dedicated Server、
cluster 和 SteamCMD 三个路径后选择“确认接管”。接管只保存管理路径，不移动存档、不重建 INI，
也不会补写缺失的 cluster 文件。探测不到默认文档目录时，可在后台设置中增加准确的父目录。

### 安装全新服务器

适用于没有 DST 世界的新机器，或希望保留现有世界并另建一套服务器的场景：

1. 填写 SteamCMD 根目录，例如 `C:\SteamCMD`。Hearth 固定使用 App `343050` 和 SteamCMD 标准
   `steamapps\common\Don't Starve Together Dedicated Server` 目录，不接受前端自定义可执行文件
   或启动参数。
2. 填写一个“父目录已经存在、目标自身尚不存在”的 cluster 绝对路径，例如
   `C:\Users\Administrator\Documents\Klei\DoNotStarveTogether\HearthCluster`。已有目录必须走
   接管；安装流程不会覆盖、合并或迁移其中的文件。
3. 填写服务器名称。Cluster Token 可以同时填写，也可以在安装成功后从托管卡片补充。
4. 勾选写入确认并开始安装。SteamCMD 若需要先更新自身，Hearth 会等待其派生进程退出并自动
   重试一次；连续无日志进展达到后台设置的超时时间后才终止进程树。
5. 任务成功后 DST 已接入 Hearth，但 Master/Caves 保持停止。没有 Token 时启动会被拒绝并给出
   明确提示。

Hearth 会创建 `cluster.ini`、`Master/server.ini`、`Caves/server.ini` 和洞穴
`worldgenoverride.lua`。这些文件先在目标父目录完整暂存，再通过同一文件系统内的一次重命名提交。
适配器初始化或 Hearth 配置保存失败时，只删除这次新建的 cluster；已经下载完成的 Steam 文件
会保留，修正输入后可以重试。

## Token 与敏感信息

Cluster Token 只会写入 `<cluster>\cluster_token.txt`。Hearth 不把明文保存到 `config.json`，
也不通过管理接口回显，不写入任务详情或安全操作审计。安装页面提交成功后会清空浏览器内的
Token 输入。更换 Token 前必须停止 DST。

## 启动、停止与端口

新集群默认使用以下本机分片端口：

- 分片通信：`10888`
- Master 游戏端口：`10999`，Steam：`27016`，认证：`8766`
- Caves 游戏端口：`11000`，Steam：`27017`，认证：`8767`

Hearth 不自动修改 Windows 防火墙或云安全组。需要公网玩家访问时，只开放实际需要的游戏端口，
不要开放 Hearth 8080、SteamCMD 或其他内部管理端口。DST 没有 Hearth 可用的 REST 保存/关闭
通道，因此停止和重启需要管理员明确接受终止两个已接管分片的风险。

## 配置、备份与更新

- 只有已托管 DST 才显示配置导航。常用服务器、玩法、分片端口和 Master/Caves 世界规则采用
  结构化配置；高级模式保留未知 INI 参数与注释。
- 独立备份只允许在 Master/Caves 停止时执行，避免归档仍在变化的存档。
- 安全更新会在管理员确认后按“停止 → 静止 cluster 备份 → SteamCMD App `343050` 更新 →
  恢复任务前运行状态 → 分片存活检查”执行。更新前已停止的服务器仍保持停止。
- SteamCMD 自身版本不显示为 DST 版本，也不参与“有新版本”的结论。

## 当前边界

1.4.0 不提供模组操作或面板内备份恢复。内部通用模组契约不会创建任何按钮、下载 Workshop 内容
或删除 Hearth 未接管的文件；Palworld 与 DST 模组能力分别在 1.4.1 和 1.4.2 验收后开放。
