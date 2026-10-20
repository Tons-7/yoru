package proc

import (
	"context"
	"encoding/base64"
	"os/exec"
	"strings"
	"unicode/utf16"
)

// PowerShell runs PowerShell with args and no window.
func PowerShell(ctx context.Context, args ...string) *exec.Cmd {
	return Hide(exec.CommandContext(ctx, "powershell", append([]string{"-NoProfile", "-NonInteractive"}, args...)...))
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
