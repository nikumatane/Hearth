<p align="center">
  <a href="ROADMAP.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Hearth roadmap

This document records the agreed boundaries of public iterations. Any scope change should
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

Status: complete on Windows (DST one-click installation, mods, and backup/restore remain outside this phase).

- The first phase supports read-only discovery and administrator adoption of an existing Don't Starve
  Together Dedicated Server and cluster, plus Master/Caves start, stop, and restart. Installation,
  mods, and backups remain later phases.
- Manage Master and Caves processes, ports, logs, and lifecycle as one logical game instance.
- Define cluster configuration, token state, mod updates, safe shutdown, backup, and restore boundaries.
  Administrators can write the DST token from Game Management. The first phase never reads or returns
  the cluster token plaintext; stop/restart and token replacement require the documented safety boundary.
- Reuse the 1.1.0 game catalog, installation task, and backend settings framework instead of creating a side path.

## 1.3.1 · Game management and DST configuration

Status: complete and released with 1.3.3.

- Show Palworld and DST configuration navigation only when the game is installed locally and managed by Hearth; discovery alone is not enough.
- Add administrator-only server settings and separate Master/Caves world rules, while retaining advanced editing for three INI files and two `worldgenoverride.lua` files. World rules use static parsing and allowlisted incremental writes and never regenerate a save automatically.
- Preserve revision checks, stopped-server writes, sensitive-value masking, and audit boundaries. Custom Lua outside the safe parser subset falls back to administrator-only advanced editing without blocking the remaining DST configuration.
- Keep DST configuration separate from the Palworld WorldOption/INI editor and do not delegate it to members.

## 1.3.2 · Generic game version management

Status: complete and released with 1.3.3.

- Abstract current version, source, check state, and available version in the game adapter so the detail page is not Palworld-specific.
- Keep Palworld on actual service-server depot/manifest checks; use the DST SteamCMD app/depot version and never expose SteamCMD's own version.
- Keep checks low-frequency, manually triggerable, auditable, and distinct from whether a game is installed or managed.

## 1.3.3 · DST version checks and safe updates

Status: complete on Windows.

- Add DST SteamCMD version checks and update tasks by reusing backup, stage progress, no-progress timeout, logs, and failure recovery.
- Before an update, stop Master/Caves, create a consistent backup, update, restore the original runtime state, and confirm
  shard health. A failure retains the new backup and attempts to restore the task's previous runtime state.
- Show update controls only for installed and managed DST; do not auto-install the game or manage mods.

## 1.4.0 · Explicit DST installation and shared mod foundations

Status: planned.

- Rename the ambiguous “automatic DST installation” capability to “administrator-confirmed one-click installation.”
  Hearth never installs a game during startup, discovery, or update; SteamCMD runs only after an administrator chooses
  a target and confirms the operation.
- Reuse the game catalog, task stages, no-progress timeout, logs, audit trail, and failure cleanup to install DST.
  A completed installation remains stopped until an administrator configures its cluster and token.
- Add a cross-game model for mod inventory, source, version, enabled state, dependency warnings, change plans, and
  rollback records. Mod actions require an installed and managed game and never delete unknown files Hearth has not adopted.

## 1.4.1 · Official Palworld mod management

Status: planned.

- Support only the official Palworld dedicated-server package and Workshop-directory format. Inspect `Info.json`,
  server compatibility, package name, version, and dependencies instead of supporting legacy loose injectors or
  undeclared arbitrary file replacement.
- Let administrators upload an official package or adopt an existing Workshop directory, then inspect, enable,
  disable, or remove a mod. Every change is planned first, backs up mod configuration and saves, runs while stopped,
  and is verified by a restart.
- Distinguish server-only, client-required, missing-dependency, and unknown-compatibility states. Hearth does not promise
  automatic conflict resolution and never stores Steam account passwords.

## 1.4.2 · DST Workshop mod management

Status: planned.

- Manage `dedicated_server_mods_setup.lua` by Workshop ID and manage declarative settings and enabled state separately
  for Master and Caves through their `modoverrides.lua` files.
- Distinguish declared-for-download, downloaded, Master-enabled, Caves-enabled, and invalid-configuration states so a
  successful download is never reported as active on both shards. Changes require stopped shards, backups, audit, and
  original-file rollback.
- The first phase excludes third-party marketplace search, account login, and arbitrary URL downloads. External
  providers require a separate plan with explicit licensing, authentication, digest verification, and supply-chain boundaries.

## Versioning rules

- These iterations preserve the existing configuration and HTTP API by default.
- Hearth moves to 2.0.0 only when the configuration schema, installation model, or public API must break, with documented migration and rollback steps.
- A feature that has not passed its target release acceptance criteria remains hidden or explicitly marked as planned, never available.
