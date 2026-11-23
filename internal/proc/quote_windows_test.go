package proc

import (
	"os/exec"
	"strings"
	"testing"
)

func TestQuoteEscapesEverySingleQuote(t *testing.T) {
	for _, s := range []string{"plain", "it's", "Yoru ‘quoted’", "‚low‛", "'; Write-Output injected; '"} {
		out, err := Hide(exec.Command("powershell", "-NoProfile", "-NonInteractive", "-EncodedCommand",
			Encoded("[Console]::OutputEncoding = [Text.Encoding]::UTF8; Write-Output "+Quote(s)))).Output()
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if got := strings.TrimRight(string(out), "\r\n"); got != s {
			t.Errorf("Quote(%q) printed %q", s, got)
		}
	}
}
