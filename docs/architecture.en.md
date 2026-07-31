<p align="center">
  <a href="architecture.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Hearth architecture decisions

## Goals

Provide update, lifecycle control, monitoring, and Palworld 1.0 configuration editing without
moving an existing Windows game installation. The panel should remain lightweight and constrain
high-risk system operations to explicit allowlists.

## Selected design

Production is deployed as one Go binary:

1. Vue produces static assets during the build.
2. The static assets are embedded in the Go binary.
3. Go provides administrator/member authentication, query APIs, login and parameter audit, task
   orchestration, and game adapters.
4. The panel runs as `SYSTEM` through a Windows startup scheduled task.
5. Hearth recognizes a currently running manually started Palworld process and takes over later
   lifecycle operations only after a user requests them.
6. SteamCMD runs with a fixed App ID and absolute paths from server configuration.
7. Version checking is an explicitly triggered, read-only SteamCMD task. It compares the local
   manifest with the public-branch Build ID, never runs SteamCMD while idle, and does not depend on
   a third-party version service.
8. Successful backups trigger centralized age and total-capacity cleanup of Hearth ZIP files. Real
   SteamCMD updates use log-progress timeouts and verify the target app's completion marker.

Frontend and backend remain separate in source code but merge for deployment. This is a better fit
for one small ECS than Node SSR, Redis, and a standalone database.

## Rejected alternatives

### PufferPanel as a base

It already provides users, a console, and templates, but the current Palworld configuration
experience would still require significant customization and taking over the existing installation
would need additional migration. Hearth references only its template model.

### Pterodactyl/Pelican

These are appropriate for multi-tenant environments and many instances, but Docker, a node daemon,
and supporting services would expand deployment and troubleshooting. They are not selected for the
current small-server use case.

## Security boundaries

- Initial deployment listens only on Windows loopback and does not change the firewall.
- The panel password is stored in a file readable only by `SYSTEM` and Administrators.
- The installer password is the sole administrator credential. Member passwords use individual
  random salts and PBKDF2-SHA256 digests in the same protected directory; plaintext is not stored.
- Production administrator passwords and new/changed member passwords require at least 10
  characters. This is a compatibility minimum; the documentation still recommends a unique password
  or passphrase of 14 characters or more.
- The password itself identifies the login; there is no username. Members are distinguished by an
  automatically generated credential ID.
- New members are read-only. Administrators can grant lifecycle control, server update, save backup,
  and routine Palworld gameplay-setting capabilities per member password. Gameplay settings use a
  conservative backend allowlist; system, security, performance, disk, unknown advanced settings,
  and the complete `WorldOption.sav` remain administrator-only.
- Legacy `palworld.settings` permission is narrowed to `palworld.settings.gameplay` when loaded so
  upgrades cannot retain an overly broad capability.
- Member management, task logs, login audit, and parameter audit are always administrator-only and
  cannot be delegated through member permissions.
- Login audit records source IP, credential ID, result, reason, attack severity, consecutive failure
  count, and time, but never the submitted password. Adding and removing IP rules uses the same
  audit stream.
- Parameter audit runs only after a successful `PalWorldSettings.ini` save and compares structured
  values before and after the write. It records actor, source IP, configuration revision, and actual
  changes. Sensitive values are marked as changed without recording their contents.
- Parameter audit uses database-free JSONL with the most recent 1,000 entries in memory. Files
  rotate at 5 MiB and retain one previous generation. A write failure is logged but does not falsely
  report that a successfully replaced configuration file failed to save.
- Login failures use exponential backoff per source: ordinary sources begin at the fifth consecutive
  failure with delays of 1, 2, 4, and 8 seconds up to five minutes. Known devices and allowlisted
  sources begin at the tenth failure and cap at 30 seconds. A successful password check clears only
  that source/device state, not an attacker's state.
- PBKDF2 verification has two ordinary-source slots and one reserved known-source slot, preventing
  request floods from exhausting CPU and normal-user capacity together. State, session, and rule
  tables all have fixed limits.
- A successful login issues a 30-day HMAC-signed `HttpOnly`, `SameSite=Strict` device cookie. It only
  selects the known-source throttle lane and cannot create a session, pass authentication, or replace
  the administrator/member password.
- IP deny rules reject before password-digest work. Allow rules only select the known-source lane and
  never bypass a password. The first release accepts exact IPv4/IPv6 addresses only and rejects
  loopback, unspecified, multicast, and trusted-proxy addresses to reduce whole-network lockouts.
  Quick deny defaults to 24 hours, allow to seven days, with a 365-day maximum or explicit permanent
  rules.
- Forwarded headers are accepted only when the direct peer belongs to `trustedProxyCidrs`. Hearth
  removes trusted proxies from the right side of `X-Forwarded-For`; malformed, oversized, or overly
  long chains fall back to the direct peer. Only loopback proxies are trusted by default.
- Entering warning/critical attack severity is explicitly audited. Repeated throttle or deny-rule
  blocks write at most once per minute so an attacker cannot expand the audit file without bound.
  Login JSONL rotates at 5 MiB with one previous generation.
- JSON API requests have explicit size limits, accept exactly one complete object, and reject trailing
  values or truncated oversized input.
- Every web-page load logs out the old browser session and asks for a password again. Deleting a
  member or changing its password or permissions immediately revokes existing sessions.
- The frontend cannot submit executable paths or arbitrary command arguments.
- Game IDs map to process names, installation directories, and Steam App IDs only through server
  configuration.
- At most one mutating task runs for the same game at a time.
- Update and backup while running must first save the world through the official Palworld REST API.
- Stop and restart always try a REST shutdown first. When REST is unavailable and the user explicitly
  accepts the save risk, Hearth may terminate only the Palworld process whose PID and start time were
  captured when the task began.
- Update performs save, graceful stop, and a critical-file ZIP backup first.
- A newly created ZIP is always retained. Older Hearth ZIPs default to 30 days and 20 GiB total.
  Cleanup failures become warnings rather than turning a successful backup into a false failure, and
  unknown filenames are never deleted.
- Forced-stop fallback does not apply to update, backup, configuration writes, or other operations.
- A running `steamcmd.exe` prevents a concurrent update.
- SteamCMD self-update, server download, and verification share log-progress monitoring. The process
  tree is stopped only after 30 minutes without log growth. A clean exit without the App ID `2394010`
  success marker retries once so self-update cannot be mistaken for a completed server update.
- Version checks share the serialized game-task lock and require Update server permission. They query
  only Steam public-branch metadata and do not persist the result.
- `WorldOption.sav` and `PalWorldSettings.ini` are read and saved as separate sources without automatic
  merge or cross-file synchronization.
- Duplicate settings show a source conflict. Save requests contain only fields explicitly changed by
  the user and include a source-file revision check.
- `WorldOption.sav` writes require a complete semantic round-trip verification.
- Configuration writes use temporary file, validation, and atomic replacement.
- Structured `PalWorldSettings.ini` responses mask passwords. Member responses also remove raw INI
  and every non-allowlisted setting. The raw `WorldOption.sav` document is administrator-only, and
  the frontend masks sensitive values after parsing. Neither source writes a password back unless it
  was explicitly entered.
- The Palworld REST API target is fixed at `127.0.0.1`; the frontend cannot control its address.

## Current acceptance criteria

- SteamCMD is not polled continuously while idle.
- Version checks are user-triggered, distinguish unchecked/checking/current/update available/
  unavailable, and do not modify server files.
- Authenticated users can see real Palworld and host status.
- Update, restart, and similar actions require confirmation and create traceable task records.
- Long tasks show stage, latest detail, and 0–100% progress.
- Backup retention is bounded by both age and total capacity without deleting the new backup or
  unknown files.
- SteamCMD self-update is not killed by a fixed total duration, while true lack of progress times out
  with a retryable error.
- Administrators can add, edit, and remove member passwords, assign capabilities per password, and
  view login-IP and parameter audit.
- Members without the matching capability cannot call process or configuration APIs. No member can
  access task logs, member management, or either audit API.
- A member with gameplay-setting permission can read and change only allowlisted INI settings, not
  raw INI, `WorldOption.sav`, or system/security settings.
- Palworld configuration supports structured editing with a raw-configuration fallback.
- Unknown Palworld settings survive a save.
- New default parameters added by a Palworld server release appear under Other 1.0 parameters without
  a Hearth upgrade.
- Common actions work at mobile width.
- The overview greeting uses the visitor browser's local time and updates while the page remains open.
- `go test ./...`, frontend type checking, and Windows amd64 cross-compilation pass.
