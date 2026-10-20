package config

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func stubWindow(t *testing.T, window func(context.Context, string) *launched) *atomic.Int32 {
	t.Helper()
	var tabs atomic.Int32

	oldWindow, oldTab := showWindow, showTab
	showWindow = func(ctx context.Context, url, _ string, _, _ bool) (*launched, string) { return window(ctx, url), "" }
	showTab = func(string) { tabs.Add(1) }
	t.Cleanup(func() { showWindow, showTab = oldWindow, oldTab })

	return &tabs
}

func TestOpenAppStopsWhenTheWindowStaysUp(t *testing.T) {
	var attempts atomic.Int32
	tabs := stubWindow(t, func(context.Context, string) *launched {
		attempts.Add(1)
		return &launched{}
	})

	OpenApp(context.Background(), "http://localhost:4321", "", nil)

	if attempts.Load() != 1 {
		t.Errorf("tried %d times, want one", attempts.Load())
	}
	if tabs.Load() != 0 {
		t.Error("opened a browser tab as well as the window")
	}
}

// After an update the old window still holds the profile, so a browser that can't take it exits blank.
func TestOpenAppRetriesOnceBeforeGivingUp(t *testing.T) {
	var attempts atomic.Int32
	tabs := stubWindow(t, func(context.Context, string) *launched {
		if attempts.Add(1) > 1 {
			return &launched{}
		}
		return nil
	})

	OpenApp(context.Background(), "http://localhost:4321", "", nil)

	if attempts.Load() != 2 {
		t.Errorf("tried %d times, want a second attempt", attempts.Load())
	}
	if tabs.Load() != 0 {
		t.Error("fell back to a tab even though the retry worked")
	}
}

// A browser that merely started is not success: one that showed nothing left no interface.
func TestOpenAppFallsBackToATab(t *testing.T) {
	var attempts atomic.Int32
	tabs := stubWindow(t, func(context.Context, string) *launched {
		attempts.Add(1)
		return nil
	})

	OpenApp(context.Background(), "http://localhost:4321", "", nil)

	if attempts.Load() != 2 {
		t.Errorf("tried %d times, want both attempts", attempts.Load())
	}
	if tabs.Load() != 1 {
		t.Errorf("opened %d tabs, want exactly one fallback", tabs.Load())
	}
}

func TestOpenAppGivesUpWhenCancelled(t *testing.T) {
	var attempts atomic.Int32
	tabs := stubWindow(t, func(context.Context, string) *launched {
		attempts.Add(1)
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	OpenApp(ctx, "http://localhost:4321", "", nil)

	if attempts.Load() != 0 || tabs.Load() != 0 {
		t.Errorf("attempts=%d tabs=%d, want nothing opened after cancellation",
			attempts.Load(), tabs.Load())
	}
}

// Quitting during the wait between attempts must not open anything.
func TestOpenAppRetryHonoursCancellation(t *testing.T) {
	var attempts atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	tabs := stubWindow(t, func(context.Context, string) *launched {
		if attempts.Add(1) == 1 {
			cancel()
		}
		return nil
	})

	done := make(chan struct{})
	go func() {
		OpenApp(ctx, "http://localhost:4321", "", nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("OpenApp did not return")
	}
	if attempts.Load() != 1 {
		t.Errorf("tried %d times after cancellation, want one", attempts.Load())
	}
	if tabs.Load() != 0 {
		t.Errorf("opened %d tabs while shutting down, want none", tabs.Load())
	}
}

// The window goes with Yoru, so the profile is free for whatever starts next.
func TestCloseEndsTheWindowProcess(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "ping -n 30 127.0.0.1 >NUL")
	} else {
		cmd = exec.Command("sleep", "30")
	}
	if err := cmd.Start(); err != nil {
		t.Skip("no long-running command available:", err)
	}
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()

	stubWindow(t, func(context.Context, string) *launched {
		return &launched{cmd: cmd, exited: exited}
	})
	w := &Window{}
	w.Open(context.Background(), "http://localhost:4321")

	start := time.Now()
	w.Close()
	select {
	case <-exited:
	default:
		t.Fatal("the window process is still running after Close")
	}
	if time.Since(start) > 3*time.Second {
		t.Error("Close waited on the timeout rather than the process")
	}

	// Closing again, or a window that was never opened, is nothing.
	w.Close()
	(&Window{}).Close()
}

// Fullscreen unless the setting asks for a maximized window.
func TestWindowModeFlag(t *testing.T) {
	has := func(args []string, flag string) bool {
		return slices.Contains(args, flag)
	}
	full := windowArgs("http://x", "p", false)
	if !has(full, "--start-fullscreen") || has(full, "--start-maximized") {
		t.Errorf("default = %v, want fullscreen", full)
	}
	max := windowArgs("http://x", "p", true)
	if !has(max, "--start-maximized") || has(max, "--start-fullscreen") {
		t.Errorf("maximized = %v", max)
	}

	var got bool
	old := showWindow
	showWindow = func(_ context.Context, _, _ string, maximized, _ bool) (*launched, string) {
		got = maximized
		return &launched{}, ""
	}
	t.Cleanup(func() { showWindow = old })
	(&Window{Maximized: func() bool { return true }}).Open(context.Background(), "http://x")
	if !got {
		t.Error("the setting did not reach the launch")
	}
}

func TestCloseHasNothingToDoAfterAHandOff(t *testing.T) {
	stubWindow(t, func(context.Context, string) *launched { return &launched{} })
	w := &Window{}
	w.Open(context.Background(), "http://localhost:4321")
	w.Close()
}

// Not a test: the stand-in browser of the tests below, up for $YORU_SLEEPER.
func TestSleeper(t *testing.T) {
	d, err := time.ParseDuration(os.Getenv("YORU_SLEEPER"))
	if err != nil {
		t.Skip("run by the window tests")
	}
	time.Sleep(d)
	code, _ := strconv.Atoi(os.Getenv("YORU_SLEEPER_CODE"))
	os.Exit(code)
}

// browserFor starts the stand-in browser, up for d, as a launched window; code is how it exits.
func browserFor(t *testing.T, d time.Duration, code ...int) *launched {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSleeper$")
	cmd.Env = append(os.Environ(), "YORU_SLEEPER="+d.String())
	if len(code) > 0 {
		cmd.Env = append(cmd.Env, "YORU_SLEEPER_CODE="+strconv.Itoa(code[0]))
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() { cmd.Process.Kill() })
	return &launched{cmd: cmd, exited: exited}
}

// A window closed by the person (its browser gone) is reported, so Yoru can exit with it.
func TestAWindowClosedByItsBrowserIsReported(t *testing.T) {
	l := browserFor(t, 300*time.Millisecond)
	stubWindow(t, func(context.Context, string) *launched { return l })
	closed := make(chan struct{})
	(&Window{Closed: func() { close(closed) }}).Open(context.Background(), "http://x")
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the window's browser exited unreported")
	}
}

// A browser that crashed didn't close the window on purpose: Yoru and its downloads keep going.
func TestACrashedBrowserIsNotAClose(t *testing.T) {
	l := browserFor(t, 300*time.Millisecond, 3)
	stubWindow(t, func(context.Context, string) *launched { return l })
	var closed atomic.Bool
	(&Window{Closed: func() { closed.Store(true) }}).Open(context.Background(), "http://x")
	<-l.exited
	time.Sleep(300 * time.Millisecond)
	if closed.Load() {
		t.Error("a crash was reported as the person closing the window")
	}
}

// Yoru closing its own window on the way out isn't the person closing it.
func TestYorusOwnCloseIsNotReported(t *testing.T) {
	l := browserFor(t, time.Minute)
	stubWindow(t, func(context.Context, string) *launched { return l })
	var closed atomic.Bool
	w := &Window{Closed: func() { closed.Store(true) }}
	w.Open(context.Background(), "http://x")
	w.Close()
	time.Sleep(300 * time.Millisecond)
	if closed.Load() {
		t.Error("Yoru's own close was reported as the person's")
	}
}
