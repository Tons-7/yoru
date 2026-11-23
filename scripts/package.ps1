[CmdletBinding()]
param(
    [string]$Version,
    [string]$OutDir,
    [switch]$Publish,
    [string]$Notes,
    [string]$NotesFile,
    [string]$SigningKey
)

$ErrorActionPreference = 'Stop'

$root = Split-Path $PSScriptRoot -Parent
if (-not $Version) { $Version = Get-Date -Format 'yyyy.MM.dd' }
if ($Version -notmatch '^\d{4}\.\d{2}\.\d{2}(\.\d+)?$') { throw "version $Version is not yyyy.MM.dd or yyyy.MM.dd.N" }
if ($Publish) {
    $latest = (gh release view --repo Tons-7/yoru --json tagName -q .tagName) -replace '^v', ''
    if ($latest -and [version]$Version -le [version]$latest) { throw "version $Version is not newer than the latest release $latest" }
}
if (-not $OutDir) { $OutDir = Join-Path $root 'dist' }
if (-not $SigningKey) {
    $SigningKey = Join-Path $env:LOCALAPPDATA 'yoru-release\update-signing.key'
    if (-not (Test-Path $SigningKey)) { $SigningKey = Join-Path $env:LOCALAPPDATA 'kuro-release\update-signing.key' }
}
if (-not (Test-Path $SigningKey)) { throw "release key not found at $SigningKey; every installed copy refuses an unsigned release" }

$platforms = @('darwin/arm64', 'darwin/amd64', 'linux/amd64', 'linux/arm64')

function Write-Step($message) { Write-Host "==> $message" -ForegroundColor Cyan }
function Write-Note($message) { Write-Host "    $message" -ForegroundColor DarkGray }

function New-Zip($zip, [hashtable]$entries) {
    $dir = Join-Path $staging ([Guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    foreach ($name in $entries.Keys) { Copy-Item $entries[$name] (Join-Path $dir $name) }
    Remove-Item $zip -ErrorAction SilentlyContinue
    Compress-Archive -Path (Join-Path $dir '*') -DestinationPath $zip
    Write-Note ("{0} ({1:N1} MB)" -f (Split-Path $zip -Leaf), ((Get-Item $zip).Length / 1MB))
}

Write-Step "build $Version"
& (Join-Path $PSScriptRoot 'build.ps1') -Version $Version
if ($LASTEXITCODE -ne 0) { throw 'build failed' }

$exe = Join-Path $root 'yoru.exe'
if (-not (Test-Path $exe)) { throw "yoru.exe not found at $exe" }

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$staging = Join-Path ([IO.Path]::GetTempPath()) ("yoru-package-" + [Guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path $staging | Out-Null

$readme = @"
Yoru $Version

Extract this zip to a folder of its own first. Run yoru.exe from there, never
from inside the zip: Windows would put it in a temporary folder and throw your
settings away every time it closes.

yoru.exe opens its own window and, on first run, asks to download ffmpeg, about
420 MB (the torrent engine is built in; an ffmpeg already on PATH is used as is).
Programs and episodes live beside yoru.exe.

Yoru ships with no torrent sites. Add yours on the setup screen, or later
under Settings -> Downloads -> Release sites.

Windows will warn that the app is unsigned. More info -> Run anyway.

Your watch history, library and settings are kept in:
    %LOCALAPPDATA%\yoru
To keep everything in this folder instead, pick a folder under Setup ->
Where things go.

Updates: Yoru checks for a new version on launch and every 6 hours, downloads
it by itself, and installs it when you press Restart to update. Only yoru.exe
is replaced; the downloaded programs, the episode cache and your settings stay.
To update by hand, replace yoru.exe, never the whole folder.

Connecting AniList is one step, in the setup guide or Settings -> Accounts.
"@

$unixReadme = @"
Yoru $Version

On a Mac the easier way is one line in Terminal, which installs Yoru.app and
opens it (no security prompts, no chmod):
    curl -fsSL https://github.com/Tons-7/yoru/releases/latest/download/install-mac.sh | sh
Yoru.app keeps its files in ~/Library/Application Support/Yoru. Moving from
this zip: copy config.toml, and cache/ for the episodes, from this folder there.

Otherwise extract this zip to a folder of its own and run yoru from there:
    ./yoru

Yoru opens in its own window when Chrome, Edge, Brave or Chromium is installed,
otherwise in your default browser.

ffmpeg: on Linux Yoru offers to download it on first run. On macOS install it
first with Homebrew: brew install ffmpeg. An ffmpeg already on PATH is used as
is. mpv, for playing outside the browser, comes from your package manager too.

macOS blocks programs from unidentified developers. Run this once in the folder:
    xattr -d com.apple.quarantine yoru
or allow Yoru under System Settings -> Privacy & Security after the first try.

Yoru ships with no torrent sites. Add yours on the setup screen, or later
under Settings -> Downloads -> Release sites.

Your watch history, library and settings are kept in ~/.yoru. To keep
everything in this folder instead, pick a folder under Setup -> Where things go.

Updates: Yoru checks for a new version on launch and every 6 hours, downloads
it by itself, and installs it when you press Restart to update. Only the yoru
program is replaced; the downloaded programs, the episode cache and your
settings stay. To update by hand, replace the yoru file, never the whole folder.

Connecting AniList is one step, in the setup guide or Settings -> Accounts.
"@

try {
    $readmeFile = Join-Path $staging 'README.txt'
    Set-Content -Path $readmeFile -Value $readme -Encoding utf8
    $unixReadmeFile = Join-Path $staging 'README-unix.txt'
    [IO.File]::WriteAllText($unixReadmeFile, ($unixReadme -replace "`r`n", "`n") + "`n")
    $license = Join-Path $root 'LICENSE.md'

    Write-Step 'full'
    $fullZip = Join-Path $OutDir "yoru-$Version.zip"
    New-Zip $fullZip @{ 'yoru.exe' = $exe; 'README.txt' = $readmeFile; 'LICENSE.md' = $license }

    Write-Step 'update'
    $updateZip = Join-Path $OutDir "yoru-$Version-update.zip"
    New-Zip $updateZip @{ 'yoru.exe' = $exe }

    $unixzip = Join-Path $staging 'unixzip.exe'
    go -C $root build -o $unixzip ./scripts/unixzip
    if ($LASTEXITCODE -ne 0) { throw 'building unixzip failed' }

    $platformZips = @()
    $appZips = @()
    foreach ($platform in $platforms) {
        $goos, $goarch = $platform -split '/'
        Write-Step "$goos/$goarch"
        $bin = Join-Path $staging "yoru-$goos-$goarch"
        & (Join-Path $PSScriptRoot 'build.ps1') -SkipWeb -Target $platform -Version $Version -Out $bin
        if ($LASTEXITCODE -ne 0) { throw "build for $platform failed" }
        $zip = Join-Path $OutDir "yoru-$Version-$goos-$goarch.zip"
        & $unixzip -out $zip -exec yoru "yoru=$bin" "README.txt=$unixReadmeFile" "LICENSE.md=$license"
        if ($LASTEXITCODE -ne 0) { throw "zip for $platform failed" }
        Write-Note ("{0} ({1:N1} MB)" -f (Split-Path $zip -Leaf), ((Get-Item $zip).Length / 1MB))
        $platformZips += $zip
        if ($goos -eq 'darwin') {
            $appZip = Join-Path $OutDir "Yoru-$Version-mac-$goarch.zip"
            & $unixzip -app -out $appZip -icon (Join-Path $root 'web\public\icon-512.png') "yoru=$bin" "LICENSE.md=$license"
            if ($LASTEXITCODE -ne 0) { throw "app zip for $platform failed" }
            Write-Note ("{0} ({1:N1} MB)" -f (Split-Path $appZip -Leaf), ((Get-Item $appZip).Length / 1MB))
            $appZips += $appZip
        }
    }

    Write-Step 'mac install script'
    $installer = Join-Path $OutDir 'install-mac.sh'
    $script = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'install-mac.sh')) -replace "`r`n", "`n" -replace '__VERSION__', $Version
    [IO.File]::WriteAllText($installer, $script)
    Write-Note (Split-Path $installer -Leaf)

    Write-Step 'checksums'
    $sums = Join-Path $OutDir 'SHA256SUMS.txt'
    $lines = foreach ($zip in @($fullZip, $updateZip) + $platformZips + $appZips + @($installer)) {
        "{0}  {1}" -f (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower(), (Split-Path $zip -Leaf)
    }
    [IO.File]::WriteAllText($sums, ($lines -join "`n") + "`n")
    Write-Note (Split-Path $sums -Leaf)

    Write-Step 'sign'
    $sig = "$sums.sig"
    $updateGo = Get-Content (Join-Path $root 'internal\update\update.go') -Raw
    $trusted = [regex]::Match($updateGo, 'signingKeyHex = "([0-9a-f]{64})"').Groups[1].Value
    if (-not $trusted) { throw 'signingKeyHex not found in internal/update/update.go' }
    go -C $root run ./scripts/signsums -key $SigningKey -sums $sums -out $sig -trust $trusted
    if ($LASTEXITCODE -ne 0) { throw 'signing failed' }
} finally {
    Remove-Item -Recurse -Force $staging -ErrorAction SilentlyContinue
}

Add-Type -AssemblyName System.IO.Compression.FileSystem
Write-Host ''
$allowed = @('yoru.exe', 'yoru', 'README.txt', 'LICENSE.md',
    'Yoru.app/Contents/MacOS/yoru', 'Yoru.app/Contents/Info.plist', 'Yoru.app/Contents/Resources/yoru.icns',
    'Yoru.app/Contents/Resources/LICENSE.md')
foreach ($zip in @($fullZip, $updateZip) + $platformZips + $appZips) {
    Write-Host "  $(Split-Path $zip -Leaf)" -ForegroundColor Green
    $archive = [IO.Compression.ZipFile]::OpenRead($zip)
    try {
        foreach ($entry in $archive.Entries) {
            Write-Host "      $($entry.FullName)" -ForegroundColor DarkGray
            if ($allowed -notcontains $entry.FullName) { throw "$(Split-Path $zip -Leaf) holds $($entry.FullName), which must not ship" }
        }
    } finally {
        $archive.Dispose()
    }
}

Write-Host ''
Write-Host "out: $OutDir" -ForegroundColor DarkGray

if ($Publish) {
    Write-Step "publish v$Version"
    if (-not $Notes) { $Notes = "Yoru $Version" }
    $notesArgs = if ($NotesFile) { @('--notes-file', (Resolve-Path $NotesFile).Path) } else { @('--notes', $Notes) }
    gh release create "v$Version" $fullZip $updateZip @platformZips @appZips $installer $sums $sig `
        --repo Tons-7/yoru --title "Yoru $Version" @notesArgs
    if ($LASTEXITCODE -ne 0) { throw 'gh release create failed' }
    Write-Host "published https://github.com/Tons-7/yoru/releases/tag/v$Version" -ForegroundColor Green
}
