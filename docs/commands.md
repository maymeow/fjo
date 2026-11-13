# Command reference

> Will be generated from the cobra command tree (IMPLEMENTATION §14).

## Global flags

| Flag | Description |
|---|---|
| `--color auto\|always\|never` | Use color in output |
| `--verbose` | Show progress details on stderr |
| `--debug` | Show sanitized diagnostic details on stderr (also `FJO_DEBUG=1`) |
| `-h, --help` | Show help |
| `-v, --version` | Show version |

## fjo version

Show version information.

```text
$ fjo version
fjo version 0.0.1
commit: abc1234def56
built: 2026-11-13T12:00:00Z
go: go1.27.4
platform: windows/amd64
```

## fjo completion

Generate shell completion scripts (bash, zsh, fish, powershell).
