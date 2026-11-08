param([Parameter(Mandatory)][string]$Version)

$ErrorActionPreference = 'Stop'
$tag = if ($Version.StartsWith('v')) { $Version } else { "v$Version" }
$plain = $tag.Substring(1)
$asset = "flyhigh-$tag-web.tar.gz"
$fork = 'Tons-7/flyhigh-engine'
$upstream = 'getflyhigh/flyhigh-engine'
$root = Split-Path $PSScriptRoot -Parent

gh repo sync $fork
if ($LASTEXITCODE -ne 0) { throw "could not sync $fork with $upstream" }

function AssetSum($repo) {
  $rows = gh release view $tag -R $repo --json assets --jq '.assets[] | [.name, .digest] | @tsv'
  foreach ($row in $rows) {
    $name, $digest = $row -split "`t"
    if ($name -eq $asset) { return ($digest -replace '^sha256:', '').ToLower() }
  }
  return $null
}

$sum = AssetSum $upstream
if (-not $sum) { throw "$upstream $tag has no $asset" }

$dir = Join-Path ([IO.Path]::GetTempPath()) "flyhigh-$plain"
if (Test-Path $dir) { Remove-Item -Recurse -Force $dir }
New-Item -ItemType Directory -Force $dir | Out-Null
gh release download $tag -R $upstream -p $asset -D $dir
if ($LASTEXITCODE -ne 0) { throw "could not download $asset" }
$file = Join-Path $dir $asset
$got = (Get-FileHash $file -Algorithm SHA256).Hash.ToLower()
if ($got -ne $sum) { throw "checksum mismatch: downloaded $got, GitHub says $sum" }

gh release view $tag -R $fork *> $null
if ($LASTEXITCODE -ne 0) {
  $notes = "Unmodified copy of the $tag web package from $upstream, kept so Yoru can always fetch the version it is pinned to. Code: Apache-2.0. Model weights: CC BY-NC 4.0 (see models/WEIGHTS_LICENSE.md inside)."
  gh release create $tag -R $fork --title "$tag (web package, unmodified)" --notes $notes $file
  if ($LASTEXITCODE -ne 0) { throw "could not create the $tag release on $fork" }
}
if ((AssetSum $fork) -ne $sum) { throw "the fork's $asset differs from upstream's" }

& "$env:SystemRoot\System32\tar.exe" -xzf $file -C $dir
if ($LASTEXITCODE -ne 0) { throw "could not unpack $asset" }
$types = Get-ChildItem $dir -Recurse -Filter 'flyhigh_web.d.ts' | Select-Object -First 1
if (-not $types) { throw "$asset has no flyhigh_web.d.ts" }
Copy-Item $types.FullName (Join-Path $root 'web/src/player/flyhigh_web.d.ts') -Force

$sources = Join-Path $root 'internal/deps/sources.go'
$text = [IO.File]::ReadAllText($sources)
$versionAt = [regex]'(FlyHighVersion\s*=\s*")[^"]*(")'
$digestAt = [regex]'(flyhighDigest\s*=\s*")[0-9a-f]*(")'
if (-not $versionAt.IsMatch($text)) { throw "FlyHighVersion not found in $sources" }
if (-not $digestAt.IsMatch($text)) { throw "flyhighDigest not found in $sources" }
$text = $versionAt.Replace($text, "`${1}$plain`${2}", 1)
$text = $digestAt.Replace($text, "`${1}$sum`${2}", 1)
[IO.File]::WriteAllText($sources, $text)
$check = [IO.File]::ReadAllText($sources)
if (-not $check.Contains("`"$plain`"") -or -not $check.Contains("`"$sum`"")) { throw "the pin in $sources was not updated" }

Remove-Item -Recurse -Force $dir
"FlyHigh pinned to $tag ($sum), types updated."
"Next: go test ./internal/deps/ ./internal/server/, npx tsc -b --force in web/, the smooth motion check, then watch an episode."
