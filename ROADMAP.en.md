<p align="center">
  <a href="ROADMAP.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Hearth roadmap

This document records the agreed boundaries of the next three public iterations. Any scope change should
be reflected here before implementation so unfinished adapters and high-risk update capabilities are not
presented as production-ready features.

## 1.1.0 · Open-source and game-management foundation

Status: complete. Repository publication is a separate maintainer decision and does not block later development.

- Adopt the MIT License and add contribution and security policies, continuous integration, and reproducible release foundations.
- Hearth performs bounded, read-only discovery at startup. It never downloads, installs, adopts, or starts a game automatically.
- When no manageable game is found, administrators see onboarding on the dashboard; members only see an unconfigured state.
- Once at least one game exists, the dashboard remains unchanged and administrators use a separate System Settings / Game Management page.
- Every adoption and installation requires an explicit administrator action, directory selection, and confirmation. Existing files and saves remain untouched by default.
- Palworld is the first production installation provider. DST may be detected and shown as planned, but cannot be installed before its production adapter exists.
- Backend settings use validation, revisions, atomic writes, and a previous-version fallback file. Secret values can be replaced but never read back.

## 1.2.0 · Safe panel self-update

Status: complete.

- Administrators can check stable or prerelease updates in the panel; updates are never installed silently.
- Hearth downloads a versioned Windows GitHub Release asset and verifies its digest and provenance before upgrading.
- A separate updater stops the Hearth scheduled task, swaps the executable, restarts it, and performs a health check.
- A failed health check restores the previous executable and configuration. Updates and rollbacks are audited.
- Panel updates never stop or modify game processes and saves.

## 1.3.0 · Production DST adapter

Status: planned.

- Add production discovery, adoption, and administrator-triggered installation for Don't Starve Together Dedicated Server.
- Manage Master and Caves processes, ports, logs, and lifecycle as one logical game instance.
- Define cluster configuration, token state, mod updates, safe shutdown, backup, and restore boundaries.
- Reuse the 1.1.0 game catalog, installation task, and backend settings framework instead of creating a side path.

## Versioning rules

- These iterations preserve the existing configuration and HTTP API by default.
- Hearth moves to 2.0.0 only when the configuration schema, installation model, or public API must break, with documented migration and rollback steps.
- A feature that has not passed its target release acceptance criteria remains hidden or explicitly marked as planned, never available.
