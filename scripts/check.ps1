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

Push-Location (Join-Path $repoRoot "backend")
try {
    Write-Host "==> go fmt"
    $formattedFiles = go fmt ./...
    if ($LASTEXITCODE -ne 0) { throw "go fmt failed with exit code $LASTEXITCODE" }
    if ($formattedFiles) { throw "go fmt changed files: $formattedFiles" }

    Invoke-Step "go vet" { go vet ./... }
    Invoke-Step "go test" { go test ./... }
    Invoke-Step "go build" { go build ./... }
}
finally {
    Pop-Location
}

$frontendPath = Join-Path $repoRoot "frontend"
if (Get-Command npm -ErrorAction SilentlyContinue) {
    Push-Location $frontendPath
    try {
        Invoke-Step "npm ci" { npm ci }
        Invoke-Step "frontend check" { npm run check }
    }
    finally {
        Pop-Location
    }
}
else {
    Invoke-Step "frontend check (Docker)" {
        docker run --rm -v "${frontendPath}:/app" -v "/app/node_modules" -w /app node:24-alpine sh -c "npm ci && npm run check"
    }
}
