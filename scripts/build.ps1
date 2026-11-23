[CmdletBinding()]
param(
    [string]$Target,
    [switch]$SkipWeb,
    [string]$Out,
    [string]$Version
)

$ErrorActionPreference = 'Continue'
$root = Split-Path $PSScriptRoot -Parent

function Fail($message) {
    Write-Host $message -ForegroundColor Red
    exit 1
}

if (-not $SkipWeb) {
    Write-Host '==> frontend' -ForegroundColor Cyan
    Push-Location (Join-Path $root 'web')
    try {
        if (-not (Test-Path 'node_modules')) { pnpm install --frozen-lockfile }
        pnpm exec tsc -b --force
        if ($LASTEXITCODE -ne 0) { Fail 'type check failed' }
        pnpm exec vite build
        if ($LASTEXITCODE -ne 0) { Fail 'frontend build failed' }
    } finally {
        Pop-Location
    }
}

Write-Host '==> binary' -ForegroundColor Cyan

$goos, $goarch = $null, $null
if ($Target) {
    $parts = $Target -split '/'
    if ($parts.Count -ne 2) { Fail "Target must look like linux/amd64, got $Target" }
    $goos, $goarch = $parts
}

$targetOS = if ($goos) { $goos } else { go env GOOS }
if (-not $Out) {
    $ext = if ($targetOS -eq 'windows') { '.exe' } else { '' }
    $suffix = if ($Target) { "-$goos-$goarch" } else { '' }
    $Out = Join-Path $root "yoru$suffix$ext"
}
$Out = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Out)

$was = @{ CGO_ENABLED = $env:CGO_ENABLED; GOOS = $env:GOOS; GOARCH = $env:GOARCH }
$env:CGO_ENABLED = '0'
if ($goos) { $env:GOOS = $goos; $env:GOARCH = $goarch }
try {
    $ldflags = '-s -w'
    if ($targetOS -eq 'windows') { $ldflags += ' -H windowsgui' }
    if ($Version) { $ldflags += " -X yoru/internal/update.Version=$Version" }
    go -C $root build -trimpath -ldflags $ldflags -o $Out ./cmd/yoru
    if ($LASTEXITCODE -ne 0) { Fail 'go build failed' }
} finally {
    foreach ($name in $was.Keys) {
        if ($null -eq $was[$name]) { Remove-Item "Env:$name" -ErrorAction SilentlyContinue } else { Set-Item "Env:$name" $was[$name] }
    }
}

$size = [math]::Round((Get-Item $Out).Length / 1MB, 1)
Write-Host "built $Out ($size MB)" -ForegroundColor Green
