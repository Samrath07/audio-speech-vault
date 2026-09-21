Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$env:GOCACHE = Join-Path $repoRoot ".cache/go-build"
New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null

function Invoke-Step {
    param(
        [Parameter(Mandatory = $true)]
        [string] $Name,

        [Parameter(Mandatory = $true)]
        [scriptblock] $Command
    )

    Write-Host "==> $Name"
    & $Command
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE"
    }
}

Invoke-Step "go fmt" { go fmt ./... }

$changedFiles = git diff --name-only -- "*.go"
if ($changedFiles) {
    Write-Error "go fmt changed files. Review and stage the formatted files before committing:`n$changedFiles"
}

Invoke-Step "go vet" { go vet ./... }
Invoke-Step "go test" { go test ./... }
Invoke-Step "go build" { go build ./... }
