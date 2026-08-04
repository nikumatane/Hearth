<p align="center">
  <a href="CONTRIBUTING.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Contributing to Hearth

Thank you for improving Hearth. Stability, data safety, and maintainability on real Windows game
servers take priority over feature count.

## Before you start

- Search existing issues first. For changes to configuration formats, saves, process control, or
  public APIs, open an issue describing behavior, risk, and rollback before implementation.
- Do not open a public issue for a vulnerability; follow the private [security policy](SECURITY.en.md).
- Follow the boundaries in the [roadmap](ROADMAP.en.md). An unfinished game adapter must remain
  explicitly planned and cannot appear production-ready.

## Local development

Go 1.26.5+, Node.js 22+, and pnpm 10+ are required:

```bash
pnpm --dir web install --frozen-lockfile
go test ./...
pnpm --dir web check
```

Run the demo API and frontend:

```bash
make dev-api
make dev-web
```

Before submitting:

```bash
go vet ./...
pnpm --dir web build
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/hearth
```

## Change requirements

- Game paths come only from server configuration or validated discovery; the frontend cannot send
  arbitrary commands.
- Discovery remains read-only and bounded. Downloads, adoption, lifecycle operations, updates, and
  configuration writes require an explicit administrator action.
- Configuration and save writes use temporary files, validation, and recoverable replacement, and
  never overwrite unrecognized data.
- Add Go tests for new behavior. UI behavior must at least pass type checking, build, and real-browser validation.
- Record user-visible changes in both changelogs. User-facing documentation is Chinese-first with a
  matching English version.
- Never commit local configuration, passwords, audit data, saves, packages, or `web/node_modules`.

A pull request should explain the problem, selected solution, risk boundary, validation, and any
manual rollback required.
