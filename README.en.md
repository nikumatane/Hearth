<p align="center">
  <img src="docs/assets/hearth-banner.svg" width="100%" alt="Hearth — A quiet home for your game servers." />
</p>

<p align="center">
  <a href="README.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

<p align="center">
  A lightweight game-server console for small self-hosted servers.<br />
  Give friends one URL and one password to check status, update, or safely restart the server.
</p>

<p align="center">
  <code>Go</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>Vue 3</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>Windows Server</code>&nbsp;&nbsp;·&nbsp;&nbsp;<code>Palworld 1.0</code>
</p>

---

![Hearth dashboard](docs/assets/dashboard.png)

## What is Hearth?

Hearth embeds its frontend and API in one Go binary. It can adopt an existing SteamCMD game in
place or install a new Palworld Dedicated Server after explicit administrator confirmation. It
does not require Docker and never adopts, starts, or moves saves automatically. The production
adapter focuses on Windows Palworld; Don't Starve Together is detectable but remains explicitly planned.

Hearth is intended for setups where:

- A few regular friends share a server and a full hosting platform would be excessive.
- The game already has existing saves, or a controlled installation is needed on a blank Windows host.
- Friends should be able to update, restart, or back up the server without waiting for the owner.
- Clear permission boundaries matter, but a database and account system do not.

## Features

| Area | Current implementation |
| --- | --- |
| Game lifecycle | Start, safe stop, restart, SteamCMD update, ZIP backup, and staged progress |
| Palworld configuration | Read and incrementally save `WorldOption.sav` / `PalWorldSettings.ini` as separate sources, with duplicate-source conflict indicators |
| Monitoring | CPU, memory, disk, process, version, low-frequency Palworld public-branch checks, save ID, online counts and display names, and uptime |
| Access control | One administrator password, up to 20 username-free member passwords, permission presets, adaptive throttling, and IP allow/deny rules |
| Audit | Administrator-only recent activity, task logs, login/attack, security-operation, and parameter audit; passwords are never logged |
| Deployment | Single Windows binary, zero-game onboarding, read-only discovery/explicit adoption, administrator-triggered install, and startup task |

### Permission model

| Action | Administrator | Member password |
| --- | :---: | :---: |
| View status | ✓ | Always allowed |
| Start, stop, restart | ✓ | Optional permission |
| Update server | ✓ | Optional permission |
| Create save backup | ✓ | Optional permission |
| Change routine Palworld gameplay settings | ✓ | Optional permission |
| Change system/security settings or `WorldOption.sav` | ✓ | — |
| Manage member passwords | ✓ | — |
| View task logs, IP rules, and all three audit views | ✓ | — |

New members are read-only by default. Administrators can start with the Read-only, Daily
management, or Server owner preset and then adjust individual permissions. Member passwords
are stored as salted PBKDF2-SHA256 digests. The frontend hides or disables unavailable actions,
while the backend independently checks every request. Deleting a member or changing its password
or permissions immediately revokes that member's active sessions.
Administrator and new/changed member passwords require at least 10 characters. This is a minimum
compatibility threshold; a unique password or passphrase of 14 characters or more is still
recommended.

The gameplay-settings permission uses a conservative backend allowlist. It covers the server
name, description, player limit, time and experience multipliers, capture and gathering rates,
difficulty, death penalty, randomization, voice, fast travel, player lists, and disabled technology
lists. Passwords, REST/RCON, cross-play, mods, performance and disk settings, unknown advanced
settings, and the complete `WorldOption.sav` remain administrator-only. Legacy
`palworld.settings` permissions are narrowed to this gameplay permission during upgrade.

Recent activity on the home page and complete task logs remain administrator-only. Members do not
see historical activity, but they can see the current stage and progress of tasks they are allowed
to start. Recent activity is held only in the current Hearth process and is cleared after a panel
restart. The panel log stays pinned on the Task logs page. Each start, update, or version-check log
is linked to its exact operation and read only when opened; running-task logs appear automatically
and task tabs are closable. Use the log files on disk for troubleshooting across panel restarts.

After a successful `PalWorldSettings.ini` save, the backend compares structured values before and
after the write and records the time, actor, source IP, configuration revision, and actual changes.
Sensitive settings are recorded only as “changed,” without their values. Records are stored in
`config-audit.jsonl`, rotated at 5 MiB with one previous generation retained, and the most recent
1,000 entries are available under Access control → Parameter audit. `WorldOption.sav` remains
administrator-only and currently does not receive per-parameter auditing. The login audit uses the
same 5 MiB rotation policy. Successful member, IP-rule, game-adoption/install-start, and backend-settings mutations use an independent
`operation-audit.jsonl` with structured actor and target fields, no passwords or digests, the most
recent 1,000 entries in memory, and the same 5 MiB one-generation rotation policy.

Login protection uses adaptive backoff per source. Ordinary sources begin at a one-second delay
after five consecutive failures and can reach five minutes. Signed known devices and allowlisted
IPs use a separate reserved verification lane and a wider threshold, so an external attack cannot
consume all password-verification capacity. “Known” affects only throttling—it is not passwordless
login. The page still requires an administrator or member password, and the device cookie cannot
access authenticated APIs. Concurrent password-digest verification is also bounded to protect the
CPU from PBKDF2 floods.

At the attack threshold, the login audit explicitly labels suspected automated attempts or brute
force. An administrator can use a login row menu to deny that login source IP for 24 hours or allow it for seven
days, or configure rules from one hour through 365 days—or permanently—on the IP rules page. Deny
rules reject before password computation; allow rules still require the correct password. Member
and IP-rule changes appear only in Security operation audit and never masquerade as login attempts.

Hearth accepts `X-Forwarded-For` and `X-Forwarded-Proto` only from proxies listed in
`trustedProxyCidrs`, and resolves the real client from the right side of the proxy chain. By default,
only loopback is trusted, which suits a reverse proxy on the same host. For a proxy on another host,
add only that proxy's fixed address or smallest practical CIDR. Never use `0.0.0.0/0`, or clients
could forge their source IP and HTTPS marker.

## Windows quick install

Requirements:

- Windows Server 2022 or a compatible release
- SteamCMD and Palworld Dedicated Server are optional; Hearth can be installed first on an empty host
- Administrator PowerShell

Build the package:

```bash
make windows-package
```

Extract `hearth-windows-amd64-v<version>.zip` and keep these files together:

```text
hearth.exe
install-windows.ps1
uninstall-windows.ps1
LICENSE
VERSION
THIRD_PARTY_NOTICES.md
THIRD_PARTY_NOTICES.zh-CN.md
```

Run in Administrator PowerShell:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\install-windows.ps1
```

The password entered by the installer is the Hearth administrator password, separate from
Palworld's `AdminPassword`. The installer only installs Hearth and its startup task; it does not
stop, start, update, or modify a running Palworld server. Startup discovery is bounded and read-only;
it neither downloads nor adopts a game.

## First game setup

1. Install Hearth and open `http://127.0.0.1:8080` inside the ECS.
2. With no game configured, administrators see dashboard onboarding. Once at least one game is
   managed, use the separate System Settings → Game Management page.
3. For an existing Palworld server, save and stop it normally, select a discovery candidate, and
   explicitly confirm adoption. For a new server, select only the SteamCMD root; Palworld is installed
   at its standard `steamapps\\common\\PalServer` path. Installation starts only after confirmation
   and does not start the server when complete.
4. Start the server from Hearth; starting does not depend on the REST API.
5. If you need player data and safe stop/restart/update/backup while the server is running, open
   the INI source, set a non-empty `AdminPassword`, enable `RESTAPIEnabled`, and confirm
   `RESTAPIPort=8212`.

Hearth always accesses the Palworld REST API through `127.0.0.1`. Starting remains available when
REST is unavailable. Stop and restart first attempt a safe shutdown and, only after an explicit
save-risk confirmation, can fall back to terminating the identified Palworld process. Update and
backup while running still require REST and never silently degrade to forced termination.

The detail-page player list shows sanitized in-game display names only. Platform accounts, Player IDs,
and User IDs are never returned by the panel API. The player limit prefers live REST metrics; when REST
is unavailable and the active world has `WorldOption.sav`, Hearth shows the limit as unknown instead of
using an INI value that WorldOption may have overridden.
Before a safe shutdown, Hearth refreshes the online count. A confirmed empty server uses a five-second
notice; online players or an unavailable count retain `shutdownWaitSeconds` (30 seconds by default),
and task detail shows the shutdown countdown.

After creating a ZIP successfully, Hearth cleans only backups it named itself: files older than 30
days are removed first, followed by the oldest files until their total size is at most 20 GiB. The
newly created backup is always retained, and manually named or third-party files are untouched.
Adjust the thresholds with `backupRetentionDays` and `backupMaxTotalGB`.

See the [Windows Palworld deployment guide](docs/windows-palworld.en.md) for the complete flow.

See the [Hearth roadmap](ROADMAP.en.md) for the agreed boundaries of the next three open-source iterations.

## Checking for a new Palworld version

The Palworld detail page shows server-version state beside Current version. Hearth attempts the first
check 30 seconds after startup and evaluates again every 15 minutes, running another check only when
the last successful result is at least six hours old. Failed automatic retries are at least one hour
apart. Check for updates still triggers it immediately. Checks are serialized tasks and never preempt
another Hearth or SteamCMD task.

The check has two internal phases: prepare SteamCMD first, then independently refresh public-branch
metadata for App ID `2394010`. The SteamCMD version is neither displayed nor used for the server
update result; the page reports only whether Palworld Dedicated Server has an update. Manual checks
require Update server permission. Results stay in the current Hearth process and reset after a panel
restart or local server Build ID change. The decision compares installed depot manifests with their
public-branch counterparts instead of the App-level Build ID, so metadata or unrelated-depot changes
do not become false Palworld server updates. No third-party version service is required.

During a real update, SteamCMD checks and applies its own update first. Hearth treats continuous log
growth as progress, so self-update, download, and verification do not cause a false timeout. By
default, the SteamCMD process tree is stopped only after 30 minutes with no log progress. If
SteamCMD exits normally after self-update without confirming the Palworld update, Hearth retries
once and announces completion only after seeing the success marker for App ID `2394010`.
An `already up to date` marker is reported as Server already current and clears stale update state.

## Local development

Requires Go 1.26.5+, Node.js 22+, and pnpm 10+.

```bash
pnpm --dir web install
make dev-api
make dev-web
```

The demo is available at `http://127.0.0.1:5173` with password `admin`.

Production build:

```bash
make build
```

The output is `bin/hearth`. You can also run the local demo directly:

```bash
HEARTH_DEMO=true HEARTH_ADMIN_PASSWORD='replace-me' ./bin/hearth
```

## Project boundaries

- The current production adapter supports only Palworld 1.0 on Windows.
- Frontend input is never executed as arbitrary shell text.
- At most one mutating task runs for a game at a time.
- SteamCMD is launched in the background only for low-frequency Palworld server-version checks; a
  busy task or existing SteamCMD process causes the automatic check to be skipped.
- Monitoring time series and version-check results are not persisted long-term.
- World settings can be viewed and edited while running, but writing requires a safe stop.
- Hearth refuses to guess when it cannot identify the active save and never binds a fixed world ID.

## Documentation

- [Windows Palworld deployment and upgrade](docs/windows-palworld.en.md)
- [Architecture and security boundaries](docs/architecture.en.md)
- [1.1.0–1.3.0 roadmap](ROADMAP.en.md)
- [Contributing](CONTRIBUTING.en.md)
- [Security reporting policy](SECURITY.en.md)
- [MIT License](LICENSE)
- [Changelog](CHANGELOG.en.md)
- [Third-party notices](THIRD_PARTY_NOTICES.md)

The public entry point belongs to the deployment layer and is not configured or maintained by
Hearth. To share the panel with friends, use an external HTTPS entry point such as Tailscale Funnel.
Do not expose Hearth port 8080 or the Palworld REST API port 8212 directly to the public Internet.
