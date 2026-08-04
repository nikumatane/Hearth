<p align="center">
  <strong>简体中文</strong>&nbsp;&nbsp;|&nbsp;&nbsp;<a href="panel-update.en.md">English</a>
</p>

# Hearth 面板安全更新

Hearth 1.2.0 起，管理员可以在“系统设置 → 面板更新”检查并确认安装新版本。检查可以使用
stable 或 prerelease 通道，但任何通道都不会静默安装。当前只在 Windows 安装环境开放
“确认并安装”；其他系统只返回版本检查状态。

## 私有仓库阶段

仓库保持 Private 时，创建仅限 `nikumatane/Hearth`、只授予 **Contents: Read** 的
fine-grained GitHub Token，并以纯文本写入：

```text
C:\ProgramData\Hearth\github-token.txt
```

该目录由安装器限制为 `SYSTEM` 与 Administrators 可读。也可在人工开发环境设置
`HEARTH_GITHUB_TOKEN`。不要把 Token 写进 `config.json`、日志、截图或发布包。页面只显示
“已配置/未配置”，API 不返回路径内容或 Token。仓库公开后删除该文件即可匿名检查和下载。

## 校验与替换顺序

1. 固定查询 `nikumatane/Hearth` 的 GitHub Release，不接受页面传入仓库地址或下载 URL。
2. 要求精确匹配 `hearth-windows-amd64-v<版本>.zip` 及其 `.sha256`，并拒绝草稿发布。
3. 校验 GitHub Release 返回的 SHA256 制品摘要，再校验随包 SHA256 和包内 `VERSION`。
   Release 元数据请求保持 30 秒总时限；附件正文使用独立 10 分钟总时限和 30 秒响应头
   时限，兼容较慢链路但不会无限挂起。失败的 `.part` 临时文件会立即清理。
4. 以路径、单文件和总体积限制安全解压到 `C:\ProgramData\Hearth\updates`。
5. 从暂存目录启动独立 `hearth-updater.exe`，面板返回请求后再优雅退出。
6. 更新器只替换 `hearth.exe` 与 `hearth-updater.exe`，启动既有 `Hearth` 计划任务，并要求
   `/api/v1/health` 返回目标版本。
7. 60 秒内未通过健康检查时恢复旧程序并再次启动；游戏进程、存档、端口和游戏配置始终
   不在替换范围内。

更新状态会显示在页面；开始、成功、失败与自动回滚写入“安全操作审计”。详细日志位于
`C:\ProgramData\Hearth\updates\panel-update.log`。若自动回滚也失败，使用上一版 ZIP
重新运行 `install-windows.ps1 -Force` 人工恢复；原 `config.json` 会继续保留。

当前信任边界是固定 GitHub 仓库、GitHub TLS/访问控制、Release 制品摘要和随包校验文件。
独立项目签名密钥能抵御更强的发布账号失陷场景，但也需要离线密钥、CI 签名和轮换流程，
不属于 1.2.0 的部署成本。
