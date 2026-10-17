// Package shortcut keeps the user's shortcuts opening Yoru after its program is renamed (kuro.exe to yoru.exe).
package shortcut

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"yoru/internal/proc"
)

// userFolders is where a person's shortcuts live: the desktop, the Start menu, and taskbar and Start pins.
const userFolders = `[Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'),
  "$env:APPDATA\Microsoft\Internet Explorer\Quick Launch\User Pinned\TaskBar",
  "$env:APPDATA\Microsoft\Internet Explorer\Quick Launch\User Pinned\StartMenu"`

// Names and places stay; an icon taken from the old program is taken from the new one.
const script = `$ErrorActionPreference = 'SilentlyContinue'
$old = %[1]s
$shell = New-Object -ComObject WScript.Shell
$n = 0
foreach ($dir in @(%[3]s)) {
  if (-not $dir -or -not (Test-Path -LiteralPath $dir)) { continue }
  foreach ($f in Get-ChildItem -LiteralPath $dir -Filter *.lnk -Recurse -File) {
    $s = $shell.CreateShortcut($f.FullName)
    if (-not [string]::Equals($s.TargetPath, $old, [StringComparison]::OrdinalIgnoreCase)) { continue }
    $s.TargetPath = %[2]s
    if ($s.IconLocation.StartsWith($old, [StringComparison]::OrdinalIgnoreCase)) { $s.IconLocation = %[2]s + ',0' }
    $s.Save()
    $n++
  }
}
$n`

// Retarget points the user's shortcuts to old at new, and says how many there were.
func Retarget(ctx context.Context, old, new string) (int, error) {
	return retarget(ctx, old, new, userFolders)
}

func retarget(ctx context.Context, old, new, folders string) (int, error) {
	out, err := proc.PowerShell(ctx, "-EncodedCommand", proc.Encoded(fmt.Sprintf(script, proc.Quote(old), proc.Quote(new), folders))).Output()
	if err != nil {
		return 0, fmt.Errorf("shortcuts: %w", err)
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}
