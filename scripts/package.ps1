<#
.SYNOPSIS
Builds the zips that get sent to someone else.

.DESCRIPTION
Installing and updating want different things:

  yoru-<version>.zip             yoru.exe and a README. First install.
  yoru-<version>-update.zip      yoru.exe alone. What the in-app updater fetches.
  yoru-<version>-<os>-<arch>.zip The macOS and Linux binaries, which their updaters fetch.
  Yoru-<version>-mac-<arch>.zip  The macOS program as Yoru.app, and install-mac.sh, which installs it
                                 (`curl … | sh`: fetched by curl it carries no quarantine flag).
  kuro-<version>-update.zip      The same program as kuro.exe, and kuro-<version>-<os>-<arch>.zip as kuro:
                                 versions from before the rename ask for these by name. Once running, the
                                 program renames itself to yoru.exe and moves the data folder over.
  SHA256SUMS.txt                 The updater refuses a zip that does not match.
  SHA256SUMS.txt.sig             Its ed25519 signature; the updater refuses a release without one.

None carries bin/, cache/ or config.toml. bin/ is over half a gigabyte and
the app downloads it itself on first run; cache/ is downloaded episodes; and
config.toml holds the AniList client secret, which must never be shipped.

Updating means replacing yoru.exe, not the folder: the folder is where the
downloaded programs, the episode cache and the credentials live. The database is in
%LOCALAPPDATA%\yoru and is migrated on startup, so watch history survives
whatever happens to the folder.

.PARAMETER Version
Stamped into the binary and the file names. Defaults to the date; a second
build the same day needs a suffix (2026.08.27.1) or installed copies will not
see it as newer.

.PARAMETER Publish
Creates the GitHub release (tag v<version>) with the zips attached, which is
what every installed copy checks. Needs `gh auth login` once.

.PARAMETER NotesFile
The release notes, a UTF-8 markdown file passed to gh untouched; read into -Notes, PowerShell 5.1 mangles non-ASCII.

.PARAMETER SigningKey
The release key's file (a hex ed25519 seed), kept outside the repository.
Default: %LOCALAPPDATA%\yoru-release\update-signing.key, else the kuro-release
one from before the rename. Back it up: without it no installed copy will accept
another update.

.EXAMPLE
./scripts/package.ps1
./scripts/package.ps1 -Version 2026.08.27 -Publish -NotesFile notes.md
#>
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
if (-not $OutDir) { $OutDir = Join-Path $root 'dist' }
if (-not $SigningKey) {
    $SigningKey = Join-Path $env:LOCALAPPDATA 'yoru-release\update-signing.key'
    if (-not (Test-Path $SigningKey)) { $SigningKey = Join-Path $env:LOCALAPPDATA 'kuro-release\update-signing.key' }
}
if (-not (Test-Path $SigningKey)) { throw "release key not found at $SigningKey; every installed copy refuses an unsigned release" }

# The platforms build.sh users run; their updaters look for these zips by name.
$platforms = @('darwin/arm64', 'darwin/amd64', 'linux/amd64', 'linux/arm64')

function Write-Step($message) { Write-Host "==> $message" -ForegroundColor Cyan }
function Write-Note($message) { Write-Host "    $message" -ForegroundColor DarkGray }

# One zip from named files, each written under the name given: the bridge zips hold the program as kuro.
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
Nothing is installed anywhere else: everything lives beside yoru.exe.

Yoru ships with no torrent sites. Add yours on the setup screen, or later
under Settings -> Downloads -> Release sites.

Windows will warn that the app is unsigned. More info -> Run anyway.

Your watch history, library and settings are kept in:
    %LOCALAPPDATA%\yoru
To keep everything in this folder instead, pick a folder under Setup ->
Where things go.

Updates: Yoru checks for a new version on launch and offers it under
Settings -> About. Only yoru.exe is replaced; the downloaded programs, the
episode cache and your settings stay. To update by hand, replace yoru.exe,
never the whole folder.

Used kuro before? Yoru is its new name. Put yoru.exe in kuro's folder in place of
kuro.exe and run it: your history, library and settings come along.

Connecting an AniList account is optional and needs a client id of your own;
config.toml, written on first run, says how.
"@

# macOS and Linux get their own: the Windows one names yoru.exe, %LOCALAPPDATA% and Windows' warning.
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

Updates: Yoru checks for a new version on launch and offers it under
Settings -> About. Only the yoru program is replaced; the downloaded programs,
the episode cache and your settings stay. To update by hand, replace the yoru
file, never the whole folder.

Used kuro before? Yoru is its new name. Put yoru in kuro's folder in place of
kuro and run it: your history, library and settings come along.

Connecting an AniList account is optional and needs a client id of your own;
config.toml, written on first run, says how.
"@

try {
    $readmeFile = Join-Path $staging 'README.txt'
    Set-Content -Path $readmeFile -Value $readme -Encoding utf8
    # LF and no BOM there, where a BOM shows as a stray character.
    $unixReadmeFile = Join-Path $staging 'README-unix.txt'
    [IO.File]::WriteAllText($unixReadmeFile, ($unixReadme -replace "`r`n", "`n") + "`n")
    # The license's Required Notice must travel with every copy.
    $license = Join-Path $root 'LICENSE.md'

    Write-Step 'full'
    $fullZip = Join-Path $OutDir "yoru-$Version.zip"
    New-Zip $fullZip @{ 'yoru.exe' = $exe; 'README.txt' = $readmeFile; 'LICENSE.md' = $license }

    Write-Step 'update'
    $updateZip = Join-Path $OutDir "yoru-$Version-update.zip"
    New-Zip $updateZip @{ 'yoru.exe' = $exe }
    $bridgeZips = @(Join-Path $OutDir "kuro-$Version-update.zip")
    New-Zip $bridgeZips[0] @{ 'kuro.exe' = $exe }

    # Compress-Archive drops the executable bit, so unzipped on a Mac or Linux the program needed chmod.
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
            # What install-mac.sh fetches: the same program as Yoru.app.
            $appZip = Join-Path $OutDir "Yoru-$Version-mac-$goarch.zip"
            & $unixzip -app -out $appZip -icon (Join-Path $root 'web\public\icon-512.png') "yoru=$bin" "LICENSE.md=$license"
            if ($LASTEXITCODE -ne 0) { throw "app zip for $platform failed" }
            Write-Note ("{0} ({1:N1} MB)" -f (Split-Path $appZip -Leaf), ((Get-Item $appZip).Length / 1MB))
            $appZips += $appZip
        }
        $bridge = Join-Path $OutDir "kuro-$Version-$goos-$goarch.zip"
        New-Zip $bridge @{ 'kuro' = $bin }
        $bridgeZips += $bridge
    }

    Write-Step 'mac install script'
    # LF, as sh reads it, whatever checkout turned the template into; the version pins what it fetches.
    $installer = Join-Path $OutDir 'install-mac.sh'
    $script = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'install-mac.sh')) -replace "`r`n", "`n" -replace '__VERSION__', $Version
    [IO.File]::WriteAllText($installer, $script)
    Write-Note (Split-Path $installer -Leaf)

    Write-Step 'checksums'
    $sums = Join-Path $OutDir 'SHA256SUMS.txt'
    $lines = foreach ($zip in @($fullZip, $updateZip) + $platformZips + $appZips + $bridgeZips + @($installer)) {
        "{0}  {1}" -f (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower(), (Split-Path $zip -Leaf)
    }
    # LF and no BOM: the updater reads it byte for byte.
    [IO.File]::WriteAllText($sums, ($lines -join "`n") + "`n")
    Write-Note (Split-Path $sums -Leaf)

    Write-Step 'sign'
    $sig = "$sums.sig"
    # Installs trust only the key in update.go; signing with another would publish a release none accepts.
    $updateGo = Get-Content (Join-Path $root 'internal\update\update.go') -Raw
    $trusted = [regex]::Match($updateGo, 'signingKeyHex = "([0-9a-f]{64})"').Groups[1].Value
    if (-not $trusted) { throw 'signingKeyHex not found in internal/update/update.go' }
    go -C $root run ./scripts/signsums -key $SigningKey -sums $sums -out $sig -trust $trusted
    if ($LASTEXITCODE -ne 0) { throw 'signing failed' }
} finally {
    Remove-Item -Recurse -Force $staging -ErrorAction SilentlyContinue
}

# A secret shipped by accident can't be unshipped: stop on any zip entry but the program, README, license.
Add-Type -AssemblyName System.IO.Compression.FileSystem
Write-Host ''
$allowed = @('yoru.exe', 'yoru', 'kuro.exe', 'kuro', 'README.txt', 'LICENSE.md',
    'Yoru.app/Contents/MacOS/yoru', 'Yoru.app/Contents/Info.plist', 'Yoru.app/Contents/Resources/yoru.icns',
    'Yoru.app/Contents/Resources/LICENSE.md')
foreach ($zip in @($fullZip, $updateZip) + $platformZips + $appZips + $bridgeZips) {
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
    gh release create "v$Version" $fullZip $updateZip @platformZips @appZips @bridgeZips $installer $sums $sig `
        --repo Tons-7/yoru --title "Yoru $Version" @notesArgs
    if ($LASTEXITCODE -ne 0) { throw 'gh release create failed' }
    Write-Host "published https://github.com/Tons-7/yoru/releases/tag/v$Version" -ForegroundColor Green
}
