<p align="center">
  <a href="windows-dst.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Windows DST deployment and upgrade

This guide explains how Hearth installs or adopts a Don't Starve Together Dedicated Server and manages
its Master/Caves shards. Hearth never installs a game during startup, discovery, or a panel update; every
write requires explicit administrator confirmation under System Settings → Game Management.

## Two entry paths

### Adopt an existing server

Use this for a server with an existing world. Stop Master/Caves normally, rescan, verify the Dedicated
Server, cluster, and SteamCMD paths, then confirm adoption. Adoption stores management paths only: it does
not move saves, rebuild INI files, or fill missing cluster files. Add the exact parent as an extra discovery
root when the default Documents location cannot be found.

### Install a new server

Use this on a host without a DST world or when an existing world must remain untouched:

1. Enter the SteamCMD root, such as `C:\SteamCMD`. Hearth fixes App `343050` and SteamCMD's standard
   `steamapps\common\Don't Starve Together Dedicated Server` location. The frontend cannot choose an
   executable or launch arguments.
2. Enter an absolute cluster path whose parent exists but whose target does not, such as
   `C:\Users\Administrator\Documents\Klei\DoNotStarveTogether\HearthCluster`. Any existing directory
   must use adoption; installation never overwrites, merges, or moves its contents.
3. Enter the server display name. The Cluster Token is optional during setup and can be added from the
   managed-game card afterward.
4. Confirm the write scope and start. If SteamCMD updates itself first, Hearth waits for its child process
   and retries once. The process tree is terminated only after the configured no-log-progress timeout.
5. Success attaches DST to Hearth but leaves both shards stopped. Starting without a token is rejected with
   an explicit message.

Hearth creates `cluster.ini`, `Master/server.ini`, `Caves/server.ini`, and the Caves
`worldgenoverride.lua`. The complete set is staged beside the target and committed with one same-filesystem
rename. Adapter initialization or Hearth configuration persistence failure removes only this new cluster;
downloaded Steam files remain for a safe retry.

## Token and sensitive data

The Cluster Token is written only to `<cluster>\cluster_token.txt`. Hearth never stores the plaintext in
`config.json`, returns it through the management API, or puts it in task detail or operation audit. The UI
clears the setup token after an accepted request. DST must be stopped before replacing a token.

## Lifecycle and ports

The generated cluster uses these default local shard ports:

- shard communication: `10888`
- Master game: `10999`, Steam: `27016`, authentication: `8766`
- Caves game: `11000`, Steam: `27017`, authentication: `8767`

Hearth does not change Windows Firewall or cloud security groups. Expose only required game ports to players;
do not expose Hearth 8080, SteamCMD, or internal management ports. DST has no REST save/shutdown channel that
Hearth can use, so stop and restart require explicit acceptance of terminating the two managed shards.

## Configuration, backup, and update

- Configuration navigation appears only for managed DST. Structured settings cover common server, gameplay,
  shard-port, and Master/Caves world rules; advanced mode preserves unknown INI keys and comments.
- Standalone backup is available only while Master/Caves are stopped, avoiding a changing save archive.
- A confirmed safe update performs stop → static cluster backup → SteamCMD App `343050` update → restore the
  previous runtime state → shard liveness check. A server already stopped before the task remains stopped.
- SteamCMD's own version is neither displayed as the DST version nor used to decide update availability.

## Current boundary

1.4.0 exposes no mod action or in-panel backup restore. The internal shared mod contract creates no button,
downloads no Workshop content, and cannot remove files Hearth does not own. Palworld and DST mod adapters open
only after their separate 1.4.1 and 1.4.2 acceptance work.
