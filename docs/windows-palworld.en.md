<p align="center">
  <a href="windows-palworld.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Hearth: Windows Palworld deployment and safe takeover

## Initial discovery baseline

A read-only discovery report on 2026-07-29 confirmed:

- Windows Server 2022 Datacenter, four logical processors, and 8 GB RAM
- SteamCMD under the current user's `Downloads\steamcmd`
- Palworld Steam App ID `2394010`
- `PalServer-Win64-Shipping-Cmd.exe` was running
- UDP 8211 was listening
- No Windows service or scheduled task was found
- No TCP 8212 listener was found
- Two `steamcmd.exe` processes existed at the time

This is a historical pre-takeover baseline, not live state after upgrade. The final two findings
meant that the REST API was probably disabled and that the existing SteamCMD sessions needed to
exit before another update.

## Recommended first deployment

Existing servers have two approaches; a blank server has a third:

1. Let Hearth recognize and take over the currently running process. Status works, but the first
   stop depends on whether that old process already has REST enabled.
2. Save and stop the game normally using the existing method, then install Hearth, update the
   configuration, and start the server from the panel for the first time.
3. Install Hearth alone, then let an administrator choose empty directories and confirm a Palworld
   installation from onboarding.

Use the second approach for an existing server and the third for a blank host. Hearth performs only
bounded, read-only discovery at startup; it never downloads, adopts, or starts a game automatically.

Before the formal install:

1. Confirm that no players are online.
2. Save the world and stop Palworld normally using the current method.
3. If a SteamCMD window is idle at its interactive prompt, enter `quit`. If it is updating, wait for
   completion.
4. Confirm in Task Manager that both Palworld and SteamCMD have exited.

## Installation

Extract the release package to a temporary Windows directory and run as Administrator:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\install-windows.ps1
```

Do not copy only an EXE from an older directory. Extract the complete ZIP so `VERSION`,
`install-windows.ps1`, and `hearth.exe` come from the same package. To upgrade an existing Hearth
installation, run `.\install-windows.ps1 -Force`. The installer verifies the version returned by
the health endpoint after startup; the login page and bottom-left node card also show the running
version. Legacy members without permission fields become read-only. The old Change Palworld
configuration permission is narrowed to Change Palworld gameplay settings and no longer grants
system-setting or `WorldOption.sav` access.

The installer:

1. Read-only checks SteamCMD, PalServer, and settings files; missing game files do not block Hearth installation.
2. Copies the panel to `C:\ProgramData\Hearth`.
3. Creates a separate panel administrator-password file and restricts its ACL.
4. Configures member-password digests, login, security-operation, and parameter audit, IP rules, and the device-cookie
   signing key while retaining existing content during upgrade.
5. Registers a startup scheduled task named `Hearth`.
6. Listens only on `127.0.0.1:8080`.
7. Verifies the panel health endpoint.
8. Writes startup, shutdown, and runtime errors to `C:\ProgramData\Hearth\panel.log`.

New configuration enables two long-running safeguards by default:

- `backupRetentionDays: 30` and `backupMaxTotalGB: 20`: after a successful new backup, Hearth
  removes its ZIPs older than 30 days and then removes the oldest remaining ZIPs until their total
  size is at most 20 GiB. The new backup and non-Hearth filenames are never removed.
- `steamCmdNoProgressMinutes: 30`: SteamCMD is considered stuck only after 30 minutes without log
  growth. Self-update, Palworld download, and file verification all refresh the timer while they
  continue to produce output.

The installer does not:

- Modify `PalWorldSettings.ini`
- Modify `WorldOption.sav` in any world directory
- Stop or restart Palworld
- Run SteamCMD
- Open a Windows Firewall port
- Change RDP or SSH

## First game management

- With no manageable game, administrators see dashboard onboarding; members see only Not configured.
- After at least one game is managed, subsequent discovery, adoption, and installation live under
  System Settings → Game Management.
- Rescan reads paths and metadata only within bounded known locations and administrator-provided
  roots, with explicit depth, directory, and candidate limits.
- Adoption requires selecting a stable candidate and confirming again. Hearth does not create or
  overwrite `PalWorldSettings.ini` during adoption and never moves existing saves.
- A new installation selects only the SteamCMD root. When `steamcmd.exe` is absent, that root must be
  empty. Palworld is installed at the standard `steamapps\\common\\PalServer` path, which must also
  be empty. Only after explicit confirmation does Hearth download SteamCMD from Valve and install
  App `2394010`; the server remains stopped afterward.
- SteamCMD is downloaded and extracted into an isolated same-volume staging directory. ZIP paths and
  expanded size are checked before activation, so an interrupted download is not treated as valid.
- DST is read-only detected and labeled Planned for 1.3.0; it cannot be installed or adopted yet.

Backend settings use a revision and reject concurrent overwrites. A successful save retains
`config.json.previous`. Runtime security changes are marked as requiring a Hearth restart and never
restart a game process.

## Updating the Hearth panel

Administrators check and explicitly install from System Settings → Panel Update. While the repository
is private, store a Contents: Read GitHub token in
`C:\ProgramData\Hearth\github-token.txt`; no token is needed once it is public. The independent updater
replaces only Hearth and restores the old program when the new health check fails. It does not stop
Palworld. See the [panel safe-update guide](panel-update.en.md) for the complete boundary and manual recovery.

## Active-save detection

Hearth does not hard-code a world ID. It detects the active world in this order whenever status or
configuration is read:

1. Read `DedicatedServerName` from
   `Pal\Saved\Config\WindowsServer\GameUserSettings.ini` and verify that the corresponding
   directory contains `Level.sav`.
2. If there is no valid explicit setting, scan `Pal\Saved\SaveGames\0`; use the result only when
   exactly one 32-character world directory contains `Level.sav`.
3. If multiple candidates exist without an explicit setting, refuse to guess and return an error.

The detected world ID and detection source appear below the version and port on the status page.

## First configuration and start

The `Panel administrator password` requested by the installer belongs to Hearth; it is separate from
Palworld's `AdminPassword`. After signing in, an administrator can add member passwords under Access
control:

The panel administrator password and new/changed member passwords require at least 10 characters.
A unique password or passphrase of 14 characters or more is recommended in practice.

- There is no username. Each password receives an automatic `M-...` ID.
- New members can only view status. Administrators can use Read-only, Daily management, or Server
  owner presets.
- Start/stop/restart, server update, backup creation, and Palworld gameplay settings can be granted
  independently.
- Gameplay settings contain only routine rules from the backend allowlist. Passwords, REST/RCON,
  cross-play, mods, performance and disk settings, unknown advanced settings, and
  `WorldOption.sav` remain administrator-only.
- Task logs, member management, login and attack, security-operation, parameter audit, and IP rules remain
  administrator-only.
- Changing a member password or permissions logs out its existing sessions. Opening or refreshing
  the page still requires another login.
- Login audit shows source IP, administrator/member credential ID, result, suspected-attack severity,
  and time without storing the submitted password.
- A login row menu can deny that login source IP for 24 hours or allow it for seven days; complete rules are
  managed on the IP rules page. Deny rules reject before password computation, while allow rules
  still require the correct password.
- Successful member, IP-rule, game adoption/install-start, and backend-settings changes go to a
  separate Security operation audit with distinct actor-source and target fields. They never appear
  as login attempts and never store a password or password digest.
- A successful login stores a signed known-device cookie. It receives only a separate throttle lane
  and cannot sign in without a password or access authenticated APIs. Refreshing the page still logs
  out the old session and asks for the password.

Related files are under `C:\ProgramData\Hearth` and inherit an ACL that allows only `SYSTEM` and
Administrators:

```text
admin-password.txt
member-credentials.json
login-audit.jsonl
operation-audit.jsonl
config-audit.jsonl
ip-rules.json
device-cookie.key
```

`device-cookie.key` signs known-device identifiers. Losing it only makes existing devices establish
trust again; member credentials are unaffected. Do not copy it to a public directory.
`ip-rules.json` stores exact IP rules. Hit counts are runtime observations and are not written to
disk for every attack request.

After each successful `PalWorldSettings.ini` save, the server records actor, source IP, time,
configuration revision, and actual parameter changes. Sensitive values appear only as “changed.”
Administrators can view the latest 1,000 entries under Access control → Parameter audit. JSONL
rotates to `.1` at 5 MiB and retains one generation. `WorldOption.sav` does not currently receive
per-parameter audit. Login audit uses the same rotation policy.

### Reverse proxy and real client IP

The installer defaults to:

```json
"trustedProxyCidrs": ["127.0.0.0/8", "::1/128"]
```

Only a same-host reverse proxy connecting from loopback may therefore provide
`X-Forwarded-For` / `X-Forwarded-Proto`. For a proxy on another host, add only its fixed address or
smallest practical network and restart Hearth. Do not trust the entire public Internet. Hearth
removes trusted proxies from the right side of the forwarded chain. A malformed header, a header
over 1 KiB, or more than eight hops falls back to the TCP peer address.

The official Palworld 1.0 REST API is not required to start the game server process; it provides player data,
save, and graceful shutdown. For player data and safe stop/restart/update/backup while running, the
INI must include at least:

```text
AdminPassword="use-a-non-empty-administrator-password"
RESTAPIEnabled=True
RESTAPIPort=8212
```

Do not expose the REST API publicly. Hearth always accesses it through `127.0.0.1`.

The server detail page uses `/v1/api/players` for online display names, but the panel API retains only
sanitized names and never returns `accountName`, `playerId`, or `userId` to the browser. Online count
comes from the player-list length, while the limit prefers `/v1/api/metrics` `maxplayernum`. If REST is
temporarily unavailable and the active save has `WorldOption.sav`, Hearth shows an unknown limit rather
than using an INI value that WorldOption may have overridden.

When REST is disabled or temporarily unavailable, stop and restart remain available but explicitly
warn that progress since the latest automatic save may be lost. After confirmation, the task tries
a safe REST shutdown first and terminates only the same Palworld PID detected when the task began if
that safe shutdown fails. A changed PID is never terminated. Update and backup while running do not
use this fallback and still require a successful REST save.

After installation:

1. Open `http://127.0.0.1:8080` in a browser inside the ECS.
2. Return to the server page and click Start server; starting does not require REST.
3. For complete management, open Palworld configuration and select the
   `PalWorldSettings.ini` source.
4. Set a non-empty `AdminPassword` and change `RESTAPIEnabled` to `True`.
5. Confirm `RESTAPIPort` is `8212` and save the INI.

The configuration page reads and writes `WorldOption.sav` and `PalWorldSettings.ini` as independent
sources. Administrators can access both sources and all settings. Authorized members see only
allowlisted INI gameplay settings and never receive raw INI. Direct API calls cannot bypass backend
checks for system or high-risk settings. Supported `WorldOption.sav` settings are grouped as:

- Server and connectivity
- Time, progression, and spawning
- Players and Pals
- Combat, events, and death
- Resources, items, and building
- Bases and guilds
- Travel and world features
- Performance and quantity limits

Duplicate settings show a source conflict, but Hearth does not automatically choose precedence or
synchronize between files. Both sources submit only settings explicitly changed by the user, and a
password is never written back unless re-entered. `WorldOption.sav` also receives a complete semantic
round-trip verification before writing; float leaves compare numerically, so `5` and `5.0` are
equivalent. Hearth creates a full ZIP backup before a write. Settings can
be viewed and edited while running, but saving requires a safe stop.

Start waits only for the game process and does not require REST. For complete management, verify REST
from PowerShell:

```powershell
Test-NetConnection 127.0.0.1 -Port 8212
```

After a successful check, use Hearth for future stop, restart, backup, and update operations.

If you keep an old process running, Hearth can still display read-only status. Until REST is verified,
update and backup while running are rejected. Stop and restart still try safe shutdown first and can
terminate only the originally identified process after the user explicitly accepts the save risk.
Normal safe shutdown refreshes the online count: a confirmed empty server uses a five-second notice,
while online players or a failed count retain `shutdownWaitSeconds` (30 seconds by default). Task
progress shows the remaining notice time.

## Checking for a new version

Current version on the detail page prefers the game version returned by the Palworld REST API. When
REST is unavailable, it shows the Build ID from local `appmanifest_2394010.acf`. These formats differ,
so Hearth does not compare a game-version string directly with a Steam Build ID.

Hearth attempts its first check 30 seconds after startup, keeps a successful result for six hours,
and only evaluates staleness every 15 minutes rather than continuously invoking SteamCMD. Failed
automatic retries are at least one hour apart. A user with Update server permission can also click
Check for updates. The task:

1. Reads local `buildid` as a result-cache key and reads manifest IDs for installed depots.
2. Confirms no other SteamCMD or Hearth task is active.
3. Starts SteamCMD separately to finish its own preparation. Its version is neither displayed nor
   used for the server-version decision.
4. Runs SteamCMD `app_info_print 2394010` to refresh metadata without downloading or modifying the
   Palworld server.
5. Reads public manifest IDs for each depot and compares only depots actually installed locally. The
   App-level `buildid` is not the update decision.
6. Displays only Server current, Palworld server update available, or Server version check unavailable,
   without placing differently formatted Build IDs beside the game version.

The task shows stage and progress. SteamCMD preparation reuses the
`steamCmdNoProgressMinutes` timeout (30 minutes by default), so continuing self-update log output is
not interrupted; the Palworld metadata query uses a two-minute no-progress timeout. Results live
only in the current Hearth process and return to Unchecked after panel restart or a local Build ID
change. A SteamCMD query failure on the current network does not affect start, stop, backup, or a
formal update.

Valve documents [`app_info_print`](https://partner.steamgames.com/doc/sdk/uploading?l=english#DebuggingBuildIssues)
as a debugging command that displays the current Steamworks app configuration. Hearth reads only the
public depot manifests from its output. If a real SteamCMD update reports App `2394010` as
`already up to date`, Hearth shows Server already current and clears stale update state; later
SteamCMD self-verification output is not treated as Palworld download progress.

## Task logs

Opening Task logs no longer scans and reads several historical files. The panel runtime log stays
pinned. If a start, update, or version-check task is running when the page opens, its linked log is
added as a closable tab. Completed logs open from their exact activity record; when an update also
restarts the server, its SteamCMD and Palworld startup logs stay grouped under that one operation.
Stop, backup, and configuration-save operations without separate console output show no View log
action. Each request reads at most the final 128 KiB, and the API accepts only files explicitly
referenced by current activity.

## Safe update flow

The update sequence is fixed:

1. Verify the local REST API and administrator password.
2. Call official `/v1/api/save`.
3. Call official `/v1/api/shutdown` and wait for process exit.
4. Write saves and configuration to a separate ZIP.
5. Confirm no other SteamCMD process exists.
6. Run the fixed command and let SteamCMD use its standard
   `steamapps\common\PalServer` directory:

   ```text
   steamcmd.exe +login anonymous +app_update 2394010 +quit
   ```

7. Wait for SteamCMD to explicitly report success for App ID `2394010`. If the first run only applies
   a SteamCMD self-update and exits cleanly, retry once; never report a self-update alone as a
   completed Palworld update.
8. Restart PalServer and verify that the process returns.

As long as the log grows during SteamCMD self-update, download, or verification, the progress timeout
does not fire. After 30 minutes without log changes, Hearth terminates this SteamCMD process tree on
Windows and asks the administrator to retry. SteamCMD validates and repairs incomplete files on the
next run. Any failed safety precondition stops the task without forcibly killing the game process.

## Rolling back the panel

Run as Administrator:

```powershell
.\uninstall-windows.ps1
```

This stops and removes only the Hearth scheduled task and retains:

- Configuration under `C:\ProgramData\Hearth`
- Palworld application and configuration
- World saves
- Backups under `panel-backups`

Rolling back Hearth therefore does not affect a currently running game.
