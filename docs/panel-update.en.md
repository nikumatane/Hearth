<p align="center">
  <a href="panel-update.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Hearth panel safe update

Starting with Hearth 1.2.0, administrators can check and explicitly install a panel release under
System Settings → Panel Update. Stable and prerelease channels are available, but neither installs
silently. Installation is enabled only on Windows; other platforms return check status only.

## While the repository is private

Create a fine-grained GitHub token restricted to `nikumatane/Hearth` with **Contents: Read** only,
then store the plain token at:

```text
C:\ProgramData\Hearth\github-token.txt
```

The installer ACL allows only `SYSTEM` and Administrators to read this directory. A development
environment may instead use `HEARTH_GITHUB_TOKEN`. Never put the token in `config.json`, logs,
screenshots, or a release package. The page reports only configured/not configured; the API never
returns the token. Delete the file once the repository is public to use anonymous access.

## Verification and replacement order

1. Query only GitHub Releases for `nikumatane/Hearth`; the page cannot supply a repository or URL.
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

The 1.2.0 trust boundary is the fixed GitHub repository, GitHub TLS/access control, the Release asset
digest, and sidecar checksum. A separate project signing key would protect a stronger account-
compromise scenario, but it also requires offline key custody, CI signing, and rotation and is not
part of the 1.2.0 operating cost.
