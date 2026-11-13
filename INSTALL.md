# Installing fjo

No binary releases or packages are published yet. Build from source.

## From source

Requirements: Go (version from `go.mod`) and Git.

```bash
git clone <repository-url> fjo
cd fjo
make build            # Linux / macOS
./scripts/build.ps1   # Windows (PowerShell)
```

The binary is written to `bin/fjo` (`bin\fjo.exe` on Windows).

## Planned

- Release archives with checksums, SBOM and signatures (Windows, Linux, macOS).
- Homebrew, Scoop, Winget, AUR, optional Debian/RPM packages.
