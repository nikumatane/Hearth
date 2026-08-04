<p align="center">
  <a href="SECURITY.md">简体中文</a>&nbsp;&nbsp;|&nbsp;&nbsp;<strong>English</strong>
</p>

# Hearth security policy

## Supported versions

Security fixes are provided only for the latest stable Hearth release. Reproduce a finding on the
latest release before reporting an issue that affects an older version. Development branches may
contain early fixes but are not production-supported releases.

## Private vulnerability reports

Use GitHub Security → **Report a vulnerability** in this repository. Do not open a public issue or
publish exploitation details before a coordinated fix. Include:

- the affected version and Windows environment;
- reproducible steps and actual impact;
- whether passwords, cookies, source IPs, arbitrary files, command execution, or game saves are involved;
- sanitized logs or a minimal reproduction that is safe to share.

Maintainers will acknowledge the report before assessing severity, remediation, and a disclosure
window. Never damage third-party data or expand access merely to demonstrate an issue.

## Secure deployment boundary

- Keep Hearth on `127.0.0.1` and access it through a controlled HTTPS entry point; do not expose port 8080 directly.
- Never expose Palworld REST API port 8212 to the public internet.
- Restrict `trustedProxyCidrs` to the smallest ranges used by the real proxy; never use `0.0.0.0/0` or `::/0`.
- Use unique long passwords, protect `C:\ProgramData\Hearth`, and apply the latest stable fixes.

See [Architecture and security boundaries](docs/architecture.en.md) for the complete model.
