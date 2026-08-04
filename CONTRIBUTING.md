<p align="center">
  <strong>简体中文</strong>&nbsp;&nbsp;|&nbsp;&nbsp;<a href="CONTRIBUTING.en.md">English</a>
</p>

# 参与 Hearth

感谢你愿意改进 Hearth。项目优先保证真实 Windows 游戏服上的稳定性、数据安全和可维护性，
功能数量不是首要目标。

## 开始之前

- 功能和修复请先搜索已有 Issue；影响配置格式、存档、进程控制或公开 API 的改动，建议先
  提 Issue 说明行为、风险与回滚方式。
- 安全漏洞不要提交公开 Issue，请按 [安全策略](SECURITY.md) 私下报告。
- 当前版本边界以 [路线图](ROADMAP.md) 为准。尚未完成的游戏适配器必须标记为计划支持，
  不能表现为可用功能。

## 本地开发

需要 Go 1.26.5+、Node.js 22+ 和 pnpm 10+：

```bash
pnpm --dir web install --frozen-lockfile
go test ./...
pnpm --dir web check
```

运行演示 API 与前端：

```bash
make dev-api
make dev-web
```

提交前还应执行：

```bash
go vet ./...
pnpm --dir web build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/hearth
```

## 变更要求

- 游戏路径只能来自服务端配置或经过校验的探测结果，不能接受前端提交的任意命令。
- 探测保持只读、有边界；下载、接管、启动、停止、更新和配置写入必须由管理员明确触发。
- 配置和存档写入应采用临时文件、验证与可回退替换，不覆盖无法识别的数据。
- 新行为应增加 Go 测试；界面行为至少通过类型检查、构建和实际浏览器验证。
- 用户可见变化同步写入中英文 CHANGELOG；可视文档以中文为主，并维护对应英文版本。
- 不提交本地 `config.json`、密码、审计数据、存档、构建包或 `web/node_modules`。

Pull Request 请写清：解决的问题、采用方案、风险边界、验证结果以及必要的手工回滚方法。
