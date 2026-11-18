# Installing fjo

No binary releases or packages are published yet. Install with Go or build from source.

## With Go

Requirements: Go (version from `go.mod`).

```bash
go install github.com/maymeow/fjo/cmd/fjo@latest   # or @v0.1.0
```

The binary goes to `$(go env GOPATH)/bin`; make sure that directory is on your `PATH`.

## From source

Requirements: Go (version from `go.mod`) and Git.

```bash
git clone https://github.com/maymeow/fjo.git
cd fjo
make build            # Linux / macOS
./scripts/build.ps1   # Windows (PowerShell)
```

The binary is written to `bin/fjo` (`bin\fjo.exe` on Windows).

## Planned

- Release archives with checksums, SBOM and signatures (Windows, Linux, macOS).
- Homebrew, Scoop, Winget, AUR, optional Debian/RPM packages.
