<p align="center">
  <strong>简体中文</strong>&nbsp;&nbsp;|&nbsp;&nbsp;<a href="SECURITY.en.md">English</a>
</p>

# Hearth 安全策略

## 支持范围

Hearth 只为最新稳定版本提供安全修复。旧版本发现同类问题时，应先升级到最新版本再验证。
开发分支用于提前修复，但不视为生产支持版本。

## 私下报告漏洞

请使用仓库的 GitHub Security → **Report a vulnerability** 创建私密安全报告，不要提交公开
Issue，也不要在修复发布前公开利用细节。报告请包含：

- 受影响版本和 Windows 环境；
- 可复现步骤与实际影响；
- 是否涉及密码、Cookie、来源 IP、任意文件、命令执行或游戏存档；
- 可安全分享的日志或最小复现，敏感数据请先脱敏。

维护者会先确认收到报告，再评估严重性、修复和披露窗口。涉及正在运行的服务器时，请不要
为复现而破坏他人数据或扩大访问范围。

## 安全部署边界

- 默认仅把 Hearth 监听在 `127.0.0.1`，通过受控 HTTPS 入口访问；不要直接暴露 8080。
- 不要向公网开放 Palworld REST API 8212。
- `trustedProxyCidrs` 只填写实际反向代理的最小地址范围，禁止 `0.0.0.0/0` 或 `::/0`。
- 使用唯一的长密码，保护 `C:\ProgramData\Hearth`，并及时安装最新稳定修复。

完整边界见 [架构与安全边界](docs/architecture.md)。
