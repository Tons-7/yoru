package proc

import (
	"context"
	"encoding/base64"
	"os/exec"
	"strings"
	"unicode/utf16"
)

func PowerShell(ctx context.Context, args ...string) *exec.Cmd {
	return Hide(exec.CommandContext(ctx, "powershell", append([]string{"-NoProfile", "-NonInteractive"}, args...)...))
}

func Encoded(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

var singleQuotes = strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’", "‚", "‚‚", "‛", "‛‛")

func Quote(s string) string { return "'" + singleQuotes.Replace(s) + "'" }
