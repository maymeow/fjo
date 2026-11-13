# Security policy

## Reporting a vulnerability

Please do not open public issues for security problems.

Private reporting contact: **TBD** (to be defined before the first public release).

Include the fjo version (`fjo version`), platform, Forgejo version and steps to reproduce.
Never include real access tokens in reports.

## Scope

Particularly relevant:

- Token disclosure (logs, error messages, redirects, config files).
- Credential storage and isolation between hosts and users.
- Unsafe Git or shell execution.
- Extension trust boundaries.

## Supported versions

Pre-1.0: only the latest development version is supported.
