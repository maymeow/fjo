<#
.SYNOPSIS
  fjo build tasks for Windows (equivalent of the Makefile targets).

.EXAMPLE
  ./scripts/build.ps1 build
  ./scripts/build.ps1 check
#>
param(
    [ValidateSet('build', 'test', 'race', 'vet', 'fmt', 'fmt-check', 'lint', 'vuln', 'check', 'clean')]
    [string]$Task = 'build'
)

$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')

$pkg = 'github.com/maymeow/fjo'

function Invoke-Native {
    param([string]$Exe, [string[]]$Arguments)
    & $Exe @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Exe $($Arguments -join ' ') failed with exit code $LASTEXITCODE" }
}

function Get-GitValue([string[]]$Arguments, [string]$Fallback) {
    $v = & git @Arguments 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $v) { return $Fallback }
    return $v.Trim()
}

function Build {
    $version = Get-GitValue @('describe', '--tags', '--always', '--dirty') 'dev'
    $commit = Get-GitValue @('rev-parse', '--short=12', 'HEAD') 'unknown'
    $date = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
    $ldflags = "-s -w -X $pkg/internal/build.Version=$version -X $pkg/internal/build.Commit=$commit -X $pkg/internal/build.Date=$date"
    $exe = (& go env GOEXE).Trim()
    Invoke-Native go @('build', '-trimpath', '-ldflags', $ldflags, '-o', "bin/fjo$exe", './cmd/fjo')
}

function FmtCheck {
    $out = & gofmt -l .
    if ($out) { Write-Error "gofmt needed:`n$($out -join "`n")" }
}

switch ($Task) {
    'build' { Build }
    'test' { Invoke-Native go @('test', './...') }
    # -race needs cgo and a C compiler (e.g. MinGW-w64 gcc) on Windows.
    'race' { $env:CGO_ENABLED = '1'; Invoke-Native go @('test', '-race', './...') }
    'vet' { Invoke-Native go @('vet', './...') }
    'fmt' { Invoke-Native gofmt @('-w', '.') }
    'fmt-check' { FmtCheck }
    'lint' { Invoke-Native staticcheck @('./...') }
    'vuln' { Invoke-Native govulncheck @('./...') }
    'check' {
        FmtCheck
        Invoke-Native go @('vet', './...')
        Invoke-Native staticcheck @('./...')
        Invoke-Native go @('test', './...')
        $env:CGO_ENABLED = '1'; Invoke-Native go @('test', '-race', './...')
        Invoke-Native govulncheck @('./...')
    }
    'clean' { Remove-Item -Recurse -Force -ErrorAction SilentlyContinue bin, dist }
}
