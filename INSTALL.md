# Installing fjo

## Prebuilt binaries

Every release from v0.2.0 on publishes archives on
[GitHub Releases](https://github.com/maymeow/fjo/releases):

| System | Archives |
|---|---|
| Linux | `fjo_<version>_linux_amd64.tar.gz`, `_linux_arm64.tar.gz`, `_linux_armv7.tar.gz` |
| macOS | `fjo_<version>_darwin_amd64.tar.gz`, `_darwin_arm64.tar.gz` |
| Windows | `fjo_<version>_windows_amd64.zip`, `_windows_arm64.zip` |

Each archive contains the `fjo` binary (`fjo.exe` on Windows), the license,
README, this file and the changelog. The release also contains
`checksums.txt` (SHA-256 checksums of the archives) and an SPDX SBOM for
each archive (`<archive>.sbom.json`). `checksums.txt` uses the format that
`sha256sum --check` and `fjo release download` read.

### Linux and macOS

```bash
version=0.2.0
archive=fjo_${version}_linux_amd64.tar.gz
curl -LO "https://github.com/maymeow/fjo/releases/download/v${version}/${archive}"
curl -LO "https://github.com/maymeow/fjo/releases/download/v${version}/checksums.txt"
sha256sum --check --ignore-missing checksums.txt   # macOS: shasum -a 256 --check --ignore-missing checksums.txt
tar -xzf "$archive" fjo
sudo install -m 0755 fjo /usr/local/bin/fjo
```

### Windows (PowerShell)

```powershell
$version = "0.2.0"
$archive = "fjo_${version}_windows_amd64.zip"
$base = "https://github.com/maymeow/fjo/releases/download/v$version"
Invoke-WebRequest "$base/$archive" -OutFile $archive
Invoke-WebRequest "$base/checksums.txt" -OutFile checksums.txt
$expected = (Get-Content checksums.txt | Where-Object { $_.EndsWith("  $archive") }).Split(" ")[0]
if ((Get-FileHash $archive -Algorithm SHA256).Hash -ne $expected) { throw "checksum mismatch" }
Expand-Archive $archive -DestinationPath "$env:LOCALAPPDATA\Programs\fjo"
```

Add `%LOCALAPPDATA%\Programs\fjo` to your `PATH`.

## With Go

Requirements: Go (version from `go.mod`).

```bash
go install github.com/maymeow/fjo/cmd/fjo@latest   # or @v0.2.0
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

- Signatures and provenance for release archives (BACKLOG B-17).
- Homebrew, Scoop, Winget, AUR, optional Debian/RPM packages.
