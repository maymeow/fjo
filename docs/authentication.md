# Authentication

> Not implemented yet (Phase 1). This page records the agreed design.

- Personal access tokens (scoped where the server supports scopes).
- Login interactively, from stdin (`--with-token`) or via `FJO_TOKEN`.
- Tokens are stored in the OS credential store (Windows Credential Manager,
  macOS Keychain, Linux Secret Service), never in `config.yaml`.
- Identity = host ID + server username (like gh). One active user per host.

```bash
fjo auth login --host git.example.com
fjo auth login --host git.example.com --with-token < token.txt
fjo auth status
fjo auth switch --host git.example.com --user deploy-bot
fjo auth logout --host git.example.com --user developer
```

Open questions: storage when no keyring is available (BACKLOG B-5),
CI token detection (B-15).
