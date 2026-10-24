package shortcut

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// On, off, and off when Task Manager disabled it; in a key of the test's own, never the real startup list.
func TestAtSignInFollowsTheStartupList(t *testing.T) {
	base := fmt.Sprintf(`Software\yoru-test-%d`, os.Getpid())
	oldRun, oldApproved := runKey, approvedKey
	runKey, approvedKey = base+`\Run`, base+`\Approved`
	t.Cleanup(func() {
		runKey, approvedKey = oldRun, oldApproved
		registry.DeleteKey(registry.CURRENT_USER, base+`\Run`)
		registry.DeleteKey(registry.CURRENT_USER, base+`\Approved`)
		registry.DeleteKey(registry.CURRENT_USER, base)
	})
	exe := `C:\Program Files\Yoru\yoru.exe`

	if on, _ := AtSignIn(exe); on {
		t.Fatal("on before it was set")
	}
	if err := SetAtSignIn(exe, true); err != nil {
		t.Fatal(err)
	}
	if on, _ := AtSignIn(exe); !on {
		t.Fatal("off after turning it on")
	}
	if on, _ := AtSignIn(`D:\elsewhere\yoru.exe`); on {
		t.Error("an entry for another copy counted as this one's")
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, approvedKey, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	k.SetBinaryValue(runName, []byte{3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	k.Close()
	if on, _ := AtSignIn(exe); on {
		t.Error("disabled in Task Manager, yet on")
	}
	if err := SetAtSignIn(exe, true); err != nil {
		t.Fatal(err)
	}
	if on, _ := AtSignIn(exe); !on {
		t.Error("turning it on again didn't undo Task Manager's Disable")
	}
	if err := SetAtSignIn(exe, false); err != nil {
		t.Fatal(err)
	}
	if on, _ := AtSignIn(exe); on {
		t.Error("still on after turning it off")
	}
	if err := SetAtSignIn(exe, false); err != nil {
		t.Errorf("turning it off twice: %v", err)
	}
}
