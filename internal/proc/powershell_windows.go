package proc

import (
	"context"
	"encoding/base64"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

const createNoWindow = 0x08000000

// PowerShell runs PowerShell with args and no window: Yoru has no console, so each run would flash one.
func PowerShell(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "powershell", append([]string{"-NoProfile", "-NonInteractive"}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}

// Encoded is what -EncodedCommand takes: UTF-16LE, base64. No quoting to get wrong.
func Encoded(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// Quote makes s a PowerShell string literal.
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
