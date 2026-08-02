<p align="center">
  <a href="CHANGELOG.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Hearth changelog

This file records user-facing changes beginning with `0.8.0`. Work not yet released stays under
Unreleased and moves to a version section when a formal package is built.

## Unreleased

None.

## 1.0.3 - 2026-08-02

- Fixed persistent false server-update notices when the Steam App Build ID changes but the Palworld
  Dedicated Server depot does not. Version checks now compare actual local and public depot manifests.
- When SteamCMD reports App `2394010` already current, the task now says Server already current and
  clears stale update state instead of treating later SteamCMD self-verification as a Palworld download.
- Task logs are now linked to their exact operation and loaded on demand. The panel log stays pinned,
  running-task logs open automatically, historical logs open from activity, non-panel tabs are closable,
  and log-free operations such as configuration saves do not show a log action.

## 1.0.2 - 2026-08-02

- Fixed false `WorldOption.sav` round-trip failures when a float such as `5` is serialized as `5.0`,
  while retaining strict full checks for unknown fields, structure, and non-float values.
- Split server-version checks into SteamCMD preparation and a separate Palworld App `2394010` query,
  removed the mixed game-version/Build-ID presentation, and added a six-hour low-frequency check.
- Safe shutdown now refreshes the online count: empty servers use a five-second notice, while online
  or unavailable counts retain the configured delay, with remaining time shown in task progress.

## 1.0.1 - 2026-08-01

- Login and attack audit no longer mixes in IP-rule mutations. Legacy mixed records remain on disk,
  but rule events are not shown as login attempts and cannot expose an action menu for the actor IP.
- Added a separate administrator security-operation audit with structured actor and target fields.
  It covers member-credential creation, updates, deletion, and IP allow/deny rule changes without
  recording passwords, digests, or other credential secrets.
- Security operations use an independent `operation-audit.jsonl`, retain the latest 1,000 entries,
  and rotate at 5 MiB with one previous generation. Windows upgrades configure and preserve it.

## 1.0.0 - 2026-07-31

- Added an on-demand check beside Current version on the Palworld detail page. SteamCMD compares the
  local Build ID with the public branch and reports stage progress in the existing task area.
- Changed the overview greeting from fixed copy to the visitor browser's local time, refreshing the
  date and time-of-day label every minute.
- Documented the safe boundary for stop/restart when REST is unavailable.
- Narrowed member configuration permission to routine allowlisted `PalWorldSettings.ini` gameplay
  settings. System, security, performance, and complete `WorldOption.sav` access remain
  administrator-only, and legacy permission is safely narrowed during upgrade.
- Added administrator parameter audit with actor, source IP, configuration revision, and actual
  structured differences after a successful save. Sensitive values are not persisted, and JSONL
  rotates at 5 MiB.
- Tightened compound INI validation to prevent hidden extra parameters, made login throttling
  source-specific, bounded the session table, and added 5 MiB one-generation rotation to login audit.
- Added age and total-capacity retention to Palworld ZIP backups, defaulting to 30 days / 20 GiB.
  The newly created backup is always retained and unknown files are never cleaned.
- Changed real SteamCMD update timeout to 30 minutes without log progress. SteamCMD self-update output
  refreshes the timer; when self-update exits before confirming Palworld completion, Hearth retries
  once and terminates a stuck task as a Windows process tree.
- Upgraded login protection to per-source exponential backoff and bounded PBKDF2 concurrency. Known
  devices use a signed cookie and reserved verification lane but still require the correct password
  and cannot create a passwordless session.
- Added suspected automation/brute-force severity, consecutive-failure counts, and IP-rule changes to
  login audit. The row menu can deny an exact IP for 24 hours or allow it for seven days.
- Added an administrator IP rules page. Deny rules reject before password computation, while allow
  rules change throttling only and never bypass the password. Rules support expiry, permanent mode,
  notes, and runtime hit counts.
- Forwarded headers are now trusted only from direct peers in `trustedProxyCidrs` and proxy chains are
  parsed from right to left, preventing clients from forging source IP or HTTPS cookie markers.
  Windows defaults to loopback proxies only.
- Standardized installer administrator passwords and new/changed member passwords on a 10-character
  minimum. Existing member digests remain valid, and longer unique passwords remain recommended.

## 0.8.0

- When REST is unavailable, stop and restart can fall back to terminating the captured Palworld
  process after the user explicitly accepts save risk. A changed PID or process start time is never
  killed.
- Added stage, detail, and percentage progress to start, stop, restart, update, and backup tasks.
- Members can see progress for tasks they are allowed to perform on overview and detail pages, while
  task logs and login audit remain administrator-only.
- Improved mobile navigation, confirmation dialogs, task state, and action layout.
- Unified the Windows package, health endpoint, and UI version indicator on `0.8.0`.
