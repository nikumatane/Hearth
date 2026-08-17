<p align="center">
  <a href="CHANGELOG.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Hearth changelog

This file records user-facing changes beginning with `0.8.0`. Work not yet released stays under
Unreleased and moves to a version section when a formal package is built.

## Unreleased

Future releases are tracked in the [roadmap](ROADMAP.en.md).

## 1.3.3 - 2026-08-17

- Pin the transitive frontend build dependency `nanoid` to `3.3.18`, which fixes the infinite-loop
  denial-of-service advisory, so the release no longer contains the High-severity version flagged by Dependabot.
- Persist a lightweight task-history index so linked logs remain reachable after a panel update or restart.
  Keep the latest 100 tasks for at most 30 days and delete their Hearth-owned log files when records expire.
  Tasks left running by the previous process are marked Interrupted rather than reported as successful. The
  selected log now refreshes on an independent fast cadence and continues following game-console output after
  the launch task itself has completed.
- Add administrator-triggered, stopped-cluster ZIP backups and safe DST updates. Standalone backups are rejected while
  Master/Caves are running because DST has no in-game save channel that can guarantee a consistent online archive.
- A confirmed running update stops both shards, archives the static cluster, updates and verifies SteamCMD App `343050`,
  restores the task's original runtime state, and confirms that both shards remain alive. A server that was stopped
  before the task remains stopped.
- Reuse the configurable no-log-progress timeout and per-task SteamCMD logs. Failures retain the backup and attempt to
  restore the original runtime state, reporting both the update and recovery error when needed. Mods are never managed,
  and Hearth does not perform an unconfirmed binary rollback.
- Backend backup age/capacity, shutdown wait, and SteamCMD timeout settings now apply to both Palworld and DST. DST
  retention deletes only Hearth-named old ZIPs and always preserves the newly created successful backup.

- Add explicit version source, update capability, and backup capability fields so overview and detail views no longer
  describe DST status as Palworld or expose actions before an adapter supports them.
- Compare DST's installed appmanifest against public depot manifests for SteamCMD App `343050`. SteamCMD may update
  itself during preparation, but its own version is neither displayed nor used as the game-version signal, and the
  check never modifies DST files.
- Support administrator-triggered and six-hour low-frequency DST checks with task stages and raw logs. Failures only
  mark version checking unavailable and do not change Master/Caves management state. A shared Steam manifest parser
  now also provides Palworld with the available Steam Build ID.

- Show Palworld and DST configuration navigation only when the corresponding game is installed and managed by Hearth.
- Add an administrator-only DST configuration page with explicit server, gameplay, maintenance, shard,
  and Master/Caves port settings. Unknown keys and comments are preserved.
- Add separate Master and Caves world rules grouped by generation, seasons/weather, resources, creatures,
  threats, and bosses. Hearth accepts only declarative static `worldgenoverride.lua` tables, updates only
  allowlisted keys, never executes Lua, and never deletes or regenerates an existing save. Advanced mode
  exposes the three INI files and two optional worldgen files.
- Enforce fixed paths, UTF-8/size/value validation, revision checks, and stopped-server writes. Passwords
  and shard keys are returned only as configured masks, and audit records never include configuration content.

## 1.3.0 - 2026-08-11

- Fixed Windows service-account discovery of the interactive user's default
  `Documents\\Klei\\DoNotStarveTogether` cluster path; bounded OneDrive document roots are included,
  while unusual locations still use the extra discovery-root setting.
- The first 1.3.0 phase adds read-only discovery and administrator adoption for an existing DST
  Dedicated Server and cluster, plus Master/Caves start/stop/restart. Administrators can write the DST
  token from Game Management; it is written only to `cluster_token.txt`, never returned or persisted in
  Hearth configuration or audit data. Stop/restart and token replacement require the documented safety boundary.

## 1.2.0 - 2026-08-10

- Added administrator-only stable/prerelease checks and explicitly confirmed installation under
  System Settings → Panel Update. Updates are never installed silently.
- Panel updates are pinned to versioned Windows assets from the public `nikumatane/Hearth` GitHub
  Release. Hearth verifies the GitHub asset digest, sidecar SHA256, exact asset name, and packaged
  `VERSION`; it no longer accepts or reads a GitHub token.
- Added the packaged `hearth-updater.exe`. It waits for the old panel to exit, replaces only Hearth,
  starts the `Hearth` scheduled task, and checks the expected health version. Failure restores the
  old program automatically. Game processes, saves, and game configuration are outside this flow.
- Update checks, starts, success, failure, and rollback are administrator operation-audit events.
  Results cross the restart through a protected one-time state file; detailed output uses
  `panel-update.log`.
- The backend settings page can select the update channel using the existing revision check and
  atomic configuration replacement; the selection takes effect after restarting Hearth.
- Fixed update-channel saves being rejected when a legacy configuration has no trusted proxies. An
  explicitly saved empty list now means trust no proxy and retains that security boundary after restart.
- Backend settings now return JSON arrays for empty discovery-root and trusted-proxy lists, while the
  UI also tolerates `null` from older versions instead of reporting a post-save `join` error.
- GitHub Release asset downloads now have a separate ten-minute total timeout instead of sharing the
  30-second metadata limit, while retaining short connection/header and artifact-size bounds and
  immediately removing failed `.part` files.
- Panel updates now map actual downloaded bytes to 15–40% with transferred-size detail. The UI wait
  covers download, restart, and rollback, polls backend stages every second, and resumes tracking a
  running update after a refresh or page re-entry.
- Fixed stale progress when the new panel becomes healthy just before the updater writes its result.
  The backend imports an unconsumed terminal result only when it matches the running version, and the
  page stops tracking only after an explicit success, failure, or rollback result.
- Fixed stale update progress surviving a new login after a panel restart invalidates the in-memory
  session. An updating page now stores only a non-sensitive resume marker, reloads the SPA, returns to
  Panel Update after login, fetches authoritative status, and cancels polling from the old page.
- Fixed prerelease checks trusting GitHub's release-list order, which could make `rc.10` appear older
  than `rc.9`. Hearth now ignores drafts and invalid tags and selects the highest semantic version
  from the latest 100 releases.
- Fixed page reloads actively deleting a valid session and update recovery issuing another hard
  reload after completion. A password-authenticated session now survives page reloads within the
  same Hearth process; an update restart still requires one login, and the known-device cookie still
  cannot create a session or bypass the password.

## 1.1.0 - 2026-08-04

- Hearth can now start with zero configured games. Startup discovery is bounded and read-only, and
  missing Palworld files no longer terminate the panel.
- Added administrator onboarding to the zero-game dashboard. Discovery, adoption, installation, and
  backend parameters move to a separate System Settings page once a game is managed; members cannot access it.
- Added explicitly confirmed adoption and fresh installation for Palworld. New installs stage the
  SteamCMD download, validate ZIP paths and size, and use SteamCMD's standard
  `steamapps\\common\\PalServer` location; the server is never started automatically.
- Windows launches `PalServer-Win64-Shipping-Cmd.exe` directly with the documented multithreading
  arguments, avoiding SteamCMD installations where the `PalServer.exe` wrapper does not spawn the
  real server. Installation and update wait for SteamCMD child processes to exit before continuing.
- Installation progress maps SteamCMD self-update and Palworld download percentages into the existing
  task stages without regressing or adding unreliable speed and remaining-time estimates.
- DST is detected read-only and labeled as planned for 1.3.0, without premature install or adoption actions.
- Added backend settings for management paths, backup retention, SteamCMD no-progress timeout, port,
  cookies, and trusted proxies, with revision checks, atomic replacement, and a previous configuration.
  Game adoption, install starts, and backend-settings saves are recorded in administrator operation audit.
- Adopted the MIT License and added bilingual roadmap, contribution and security policies, plus a GitHub Actions CI baseline.

## 1.0.4 - 2026-08-03

- Added real online display names to the Palworld detail page. The panel API returns only sanitized
  display names and never exposes platform accounts, Player IDs, or User IDs. Player data is renamed
  to the clearer Player-count source.
- The player limit prefers live REST metrics `maxplayernum`. When REST is unavailable and the active
  world has `WorldOption.sav`, Hearth now reports the limit as unknown instead of falling back to a
  potentially stale INI value.

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
