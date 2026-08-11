<p align="center">
  <a href="panel-update.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Hearth panel safe update

Starting with Hearth 1.2.0, administrators can check and explicitly install a panel release under
System Settings → Panel Update. Stable and prerelease channels are available, but neither installs
silently. Installation is enabled only on Windows; other platforms return check status only.

## Verification and replacement order

1. Query only GitHub Releases for `nikumatane/Hearth`; the page cannot supply a repository or URL.
   The prerelease channel ignores drafts and invalid tags, then selects the highest semantic version
   from the latest 100 releases instead of trusting GitHub's page or API ordering.
2. Require exact `hearth-windows-amd64-v<version>.zip` and `.sha256` assets and reject drafts.
3. Verify the GitHub asset SHA256 digest, sidecar SHA256, and packaged `VERSION`. Release metadata
   keeps a 30-second total timeout. Asset bodies use a separate ten-minute total timeout and a
   30-second response-header timeout for slow links without hanging indefinitely; failed `.part`
   files are removed immediately.
4. Safely extract into `C:\ProgramData\Hearth\updates` with path and size limits.
5. Start the staged independent `hearth-updater.exe`; Hearth exits gracefully only after responding.
6. Replace only `hearth.exe` and `hearth-updater.exe`, start the existing `Hearth` scheduled task,
   and require `/api/v1/health` to report the target version.
7. Restore and restart the old program when the health check does not pass within 60 seconds. Game
   processes, saves, ports, and game configuration never enter the replacement flow.

The page displays progress. Start, success, failure, and automatic rollback are Security operation
audit events. Detailed output is stored in
`C:\ProgramData\Hearth\updates\panel-update.log`. If rollback also fails, rerun the previous ZIP's
`install-windows.ps1 -Force`; the existing `config.json` remains preserved.
Actual downloaded bytes map to 15–40%. The page waits up to 15 minutes and polls backend state every
second; refreshing or revisiting the page resumes tracking an unfinished update instead of treating
an old 15% value as the final result. Target-version health means only that the new process has
started. Tracking ends after the independent updater publishes an explicit matching success,
failure, or rollback result; the new process also imports an unconsumed result written after startup.
Panel login sessions remain process-memory only, so an update restart requires the password once;
an ordinary page reload in the same Hearth process restores a still-valid active session. An updating
page stores a credential-free resume marker and reloads when that session expires. After login it
returns to Panel Update, fetches terminal status instead of reusing old in-memory progress, and does
not hard-reload again when the result completes.

The 1.2.0 trust boundary is the fixed GitHub repository, GitHub TLS/access control, the Release asset
digest, and sidecar checksum. A separate project signing key would protect a stronger account-
compromise scenario, but it also requires offline key custody, CI signing, and rotation and is not
part of the 1.2.0 operating cost.

## Updating a test package

The panel updater consumes public GitHub Releases anonymously and does not read a GitHub token. To
make a test package available from the panel:

1. Create a public prerelease for the target version (for example `v1.3.0-rc.1`) and upload exactly
   `hearth-windows-amd64-v1.3.0-rc.1.zip` plus its `.sha256` sidecar. The ZIP's `VERSION` must match.
2. Select `Prerelease` under System Settings → Backend Settings, save, and restart Hearth when prompted.
3. Open System Settings → Panel Update, check for updates, verify the version and checksums, then
   explicitly prepare the update.

An unpublished local branch or ZIP is not read directly by the panel. Install it manually with the
 installer/remote copy, or publish it as a prerelease first. Validate prereleases on a test machine;
 keep production on Stable.
