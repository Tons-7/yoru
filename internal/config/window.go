package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Window is the app window Yoru opened. Closing it with Yoru frees the browser profile, so a relaunch takes it
// at once instead of racing a dying window.
type Window struct {
	// Profile is the browser profile directory; empty means AppData.
	Profile string
	// Maximized opens maximized instead of fullscreen, read at each open.
	Maximized func() bool
	// Log, if set, gets how each open went.
	Log *slog.Logger
	// Closed, if set, hears the window closed from its side (its browser exited), not by Close.
	Closed func()

	// handoff: a second launch, whose browser exits clean at once because the running window took the URL.
	handoff bool

	mu      sync.Mutex
	run     *launched
	opening bool
}

func (w *Window) profile() string {
	if w.Profile != "" {
		return w.Profile
	}
	return appProfileDir()
}

type launched struct {
	cmd    *exec.Cmd
	exited chan struct{}
}

// OpenApp, from a second launch, hands the URL to the running Yoru's window (a clean quick exit).
func OpenApp(ctx context.Context, url, profile string, log *slog.Logger) {
	(&Window{Profile: profile, handoff: true, Log: log}).Open(ctx, url)
}

// Open shows the interface in Chromium's app mode (native-looking, no cgo); every step is best-effort.
func (w *Window) Open(ctx context.Context, url string) {
	// One at a time: the tray's Open during the first launch would make a second window.
	w.mu.Lock()
	if w.opening {
		w.mu.Unlock()
		return
	}
	w.opening = true
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.opening = false
		w.mu.Unlock()
	}()

	// The server has only just bound its port; a window that opens first shows a connection error.
	select {
	case <-ctx.Done():
		return
	case <-time.After(250 * time.Millisecond):
	}

	// Twice: right after an update the old window still holds the profile.
	for attempt := range 2 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryWindowAfter):
			}
		}
		maximized := w.Maximized != nil && w.Maximized()
		l, how := showWindow(ctx, url, w.profile(), maximized, w.handoff)
		w.log("app window", "attempt", attempt+1, "maximized", maximized, "handoff", w.handoff, "outcome", how)
		if l != nil {
			w.mu.Lock()
			w.run = l
			w.mu.Unlock()
			if l.cmd != nil {
				go w.watch(l)
			}
			return
		}
	}
	w.log("app window didn't stay open; opened the default browser instead")
	showTab(url)
}

// watch reports the window closing by itself; Close forgets it first, so its own kill isn't reported.
func (w *Window) watch(l *launched) {
	<-l.exited
	w.mu.Lock()
	mine := w.run == l
	if mine {
		w.run = nil
	}
	w.mu.Unlock()
	if !mine {
		return
	}
	// A crash isn't the person closing it: Yoru and its downloads carry on, and Show opens a new one.
	if st := l.cmd.ProcessState; st != nil && !st.Success() {
		w.log("app window's browser stopped; Yoru keeps running", "state", st.String())
		return
	}
	w.log("app window closed")
	if w.Closed != nil {
		w.Closed()
	}
}

// Show brings the window to the front, or opens one when none is up.
func (w *Window) Show(ctx context.Context, url string) {
	w.mu.Lock()
	l := w.run
	w.mu.Unlock()
	if l != nil && l.cmd != nil && l.cmd.Process != nil && toFront(l.cmd.Process.Pid) {
		return
	}
	w.Open(ctx, url)
}

func (w *Window) log(msg string, args ...any) {
	if w.Log != nil {
		w.Log.Info(msg, args...)
	}
}

// Close ends the window. Bounded: shutdown must not hang on a browser that ignores the kill.
func (w *Window) Close() {
	w.mu.Lock()
	l := w.run
	w.run = nil
	w.mu.Unlock()
	if l == nil || l.cmd == nil || l.cmd.Process == nil {
		return
	}
	l.cmd.Process.Kill()
	select {
	case <-l.exited:
	case <-time.After(3 * time.Second):
	}
}

const retryWindowAfter = 1500 * time.Millisecond

// Swapped in tests, which must not open a browser.
var (
	showWindow = appWindow
	showTab    = openBrowser
)

// appWindow starts the window and reports what stayed up (no close command for a hand-off), and how it went.
func appWindow(ctx context.Context, url, profile string, maximized, handoff bool) (*launched, string) {
	browser := findChromium()
	if browser == "" {
		return nil, "no Chromium browser found"
	}

	cmd := exec.Command(browser, windowArgs(url, profile, maximized)...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Sprintf("%s didn't start: %v", browser, err)
	}
	started := time.Now()

	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()

	select {
	case <-exited:
		how := fmt.Sprintf("%s exited after %d ms with %s", browser, time.Since(started).Milliseconds(), cmd.ProcessState)
		// Off Windows the window outlives Yoru, so a relaunch hands the URL to it and exits clean at once;
		// that is the window, not a failure.
		if (runtime.GOOS != "windows" || handoff) && cmd.ProcessState.Success() {
			return &launched{}, how + ", handing the window to the one running"
		}
		return nil, how
	case <-ctx.Done():
		return &launched{cmd: cmd, exited: exited}, browser + " starting as Yoru stops"
	case <-time.After(3 * time.Second):
		return &launched{cmd: cmd, exited: exited}, browser + " up"
	}
}

func windowArgs(url, profile string, maximized bool) []string {
	// Fullscreen from the start (F11 leaves it) unless asked for a maximized
	// window; the size is the restore/fallback size if a browser ignores both.
	start := "--start-fullscreen"
	if maximized {
		start = "--start-maximized"
	}
	return []string{
		"--app=" + url,
		// Without a profile of its own the window joins an existing browser session and app mode is ignored.
		"--user-data-dir=" + profile,
		start,
		"--window-size=1440,900",
		"--no-first-run",
		"--no-default-browser-check",
	}
}

func findChromium() string {
	var candidates []string

	switch runtime.GOOS {
	case "windows":
		candidates = []string{
			`C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`,
			`C:\Program Files (x86)\BraveSoftware\Brave-Browser\Application\brave.exe`,
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		}
	case "darwin":
		// System-wide first, then a per-user install in ~/Applications.
		home, _ := os.UserHomeDir()
		for _, apps := range []string{"/Applications", filepath.Join(home, "Applications")} {
			for _, app := range []string{"Google Chrome", "Microsoft Edge", "Brave Browser", "Chromium"} {
				candidates = append(candidates, filepath.Join(apps, app+".app", "Contents", "MacOS", app))
			}
		}
	default:
		for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
			if path, err := exec.LookPath(name); err == nil {
				return path
			}
		}
		return ""
	}

	for _, path := range candidates {
		if exists(path) {
			return path
		}
	}
	return ""
}

// openBrowser is the fallback: whatever the system considers the default.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// rundll32 avoids cmd's parsing of & in a URL.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	detach(cmd)
	_ = cmd.Start()
}
