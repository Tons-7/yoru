package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yoru/internal/config"
	"yoru/internal/db"
	"yoru/internal/store"
)

// --no-window is forwarded, never added, so a windowed kuro reopens its UI.
func TestRelaunchForwardsTheWindowChoice(t *testing.T) {
	for _, tc := range []struct {
		current []string
		want    []string
	}{
		{nil, []string{"--wait-for", "42"}},
		{[]string{"--no-window", "--other"}, []string{"--wait-for", "42", "--no-window", "--other"}},
		{[]string{"--wait-for", "7", "--no-window"}, []string{"--wait-for", "42", "--no-window"}},
	} {
		got := relaunchArgs(42, tc.current)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("relaunchArgs(42, %v) = %v, want %v", tc.current, got, tc.want)
		}
	}
}

// With the child's output on NUL, a handover that failed took the app away without a word.
func TestRelaunchKeepsTheOutputItWasStartedWith(t *testing.T) {
	cmd := relaunchCmd(filepath.Join(t.TempDir(), config.ExeName("kuro")))

	if cmd.Stdout != os.Stdout {
		t.Errorf("stdout = %v, want the parent's", cmd.Stdout)
	}
	if cmd.Stderr != os.Stderr {
		t.Errorf("stderr = %v, want the parent's", cmd.Stderr)
	}
	if len(cmd.Args) < 3 || cmd.Args[1] != "--wait-for" {
		t.Errorf("args = %v, want it to wait for this process", cmd.Args)
	}
	if cmd.Dir != filepath.Dir(cmd.Path) {
		t.Errorf("dir = %q, want the executable's own directory", cmd.Dir)
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"2026.08.27", "2026.08.24", true},
		{"v2026.08.27", "2026.08.27", false},
		{"2026.08.27.1", "2026.08.27", true},
		{"2026.09.01", "2026.08.27.3", true},
		{"2026.08.27", "dev", false},
		{"", "2026.08.27", false},
	} {
		if got := Newer(tc.a, tc.b); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.Migrate(); err != nil {
		t.Fatal(err)
	}
	return store.New(conn)
}

// fakeGitHub serves one release with a stand-in executable, signed by a test key the updater trusts.
func fakeGitHub(t *testing.T, version string, exeBody []byte, sums bool) *httptest.Server {
	return fakeGitHubSigned(t, version, exeBody, sums, true)
}

func fakeGitHubSigned(t *testing.T, version string, exeBody []byte, sums, trusted bool) *httptest.Server {
	t.Helper()

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	f, _ := zw.Create(config.ExeName(config.Name))
	f.Write(exeBody)
	zw.Close()
	sum := sha256.Sum256(archive.Bytes())
	lines := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), AssetName(version))

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if trusted {
		was := signingKey
		signingKey = pub
		t.Cleanup(func() { signingKey = was })
	}
	sig := ed25519.Sign(priv, []byte(lines))

	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		assets := []map[string]string{
			{"name": AssetName(version), "browser_download_url": srv.URL + "/dl/" + AssetName(version)},
		}
		if sums {
			assets = append(assets, map[string]string{"name": sumsFile, "browser_download_url": srv.URL + "/dl/" + sumsFile},
				map[string]string{"name": sigFile, "browser_download_url": srv.URL + "/dl/" + sigFile})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": "v" + version, "body": "notes", "assets": assets})
	})
	mux.HandleFunc("/dl/"+AssetName(version), func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive.Bytes())
	})
	mux.HandleFunc("/dl/"+sumsFile, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, lines)
	})
	mux.HandleFunc("/dl/"+sigFile, func(w http.ResponseWriter, r *http.Request) {
		w.Write(sig)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A release signed by any other key is not kuro's, whatever its checksums say.
func TestApplyRefusesAReleaseSignedByAStranger(t *testing.T) {
	setVersion(t, "2026.08.24")
	st := testStore(t)
	gh := fakeGitHubSigned(t, "2026.08.27", []byte("new binary"), true, false)
	exe := filepath.Join(t.TempDir(), config.ExeName("kuro"))
	os.WriteFile(exe, []byte("old binary"), 0o755)
	u := newUpdater(t, st, gh, exe)
	u.launch = func(string) error { return nil }
	if _, err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for u.Status().Stage != StageFailed && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	st2 := u.Status()
	if st2.Stage != StageFailed || !strings.Contains(st2.Error, "signed") {
		t.Fatalf("status = %+v, want the update refused for its signature", st2)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Fatal("the executable was replaced")
	}
}

func setVersion(t *testing.T, v string) {
	t.Helper()
	was := Version
	Version = v
	t.Cleanup(func() { Version = was })
}

func newUpdater(t *testing.T, st *store.Store, gh *httptest.Server, exe string) *Updater {
	t.Helper()
	u := New(st, exe, filepath.Join(t.TempDir(), "staging"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	u.api = gh.URL
	return u
}

func TestCheckNotifiesOncePerVersion(t *testing.T) {
	setVersion(t, "2026.08.24")
	st := testStore(t)
	gh := fakeGitHub(t, "2026.08.27", []byte("new"), true)
	u := newUpdater(t, st, gh, filepath.Join(t.TempDir(), "kuro.exe"))
	ctx := context.Background()

	for range 2 {
		got, err := u.Check(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Available || got.Latest == nil || got.Latest.Version != "2026.08.27" {
			t.Fatalf("status = %+v, want 2026.08.27 available", got)
		}
	}

	items, _ := st.Notifications(ctx, false, 10)
	if len(items) != 1 || items[0].Kind != store.NotifyUpdate {
		t.Fatalf("notifications = %+v, want exactly one update notice", items)
	}
}

func TestDevBuildNeverSeesAnUpdate(t *testing.T) {
	setVersion(t, "dev")
	st := testStore(t)
	gh := fakeGitHub(t, "2026.08.27", []byte("new"), true)
	u := newUpdater(t, st, gh, filepath.Join(t.TempDir(), "kuro.exe"))

	got, err := u.Check(context.Background())
	if err != nil || got.Available {
		t.Fatalf("status = %+v err = %v, want nothing offered to a dev build", got, err)
	}
	if err := u.Apply(); err == nil {
		t.Fatal("apply must refuse when nothing is available")
	}
}

func TestApplySwapsTheExecutableAndHandsOver(t *testing.T) {
	setVersion(t, "2026.08.24")
	st := testStore(t)
	gh := fakeGitHub(t, "2026.08.27", []byte("new binary"), true)

	exe := filepath.Join(t.TempDir(), config.ExeName("kuro"))
	os.WriteFile(exe, []byte("old binary"), 0o755)

	var launched string
	restarted := make(chan struct{})
	u := newUpdater(t, st, gh, exe).WithRestart(func() { close(restarted) })
	u.launch = func(path string) error { launched = path; return nil }

	if _, err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-restarted:
	case <-time.After(10 * time.Second):
		t.Fatalf("no restart; status = %+v", u.Status())
	}

	if got, _ := os.ReadFile(exe); string(got) != "new binary" {
		t.Errorf("executable = %q, want the new one in place", got)
	}
	if got, _ := os.ReadFile(exe + ".old"); string(got) != "old binary" {
		t.Errorf("old binary = %q, want it kept aside until the next start", got)
	}
	if launched != exe {
		t.Errorf("launched %q, want the replaced executable", launched)
	}
	if st := u.Status(); st.Stage != StageRestarting || st.Bytes == 0 {
		t.Errorf("status = %+v", st)
	}

	Cleanup(exe)
	if _, err := os.Stat(exe + ".old"); err == nil {
		t.Error("cleanup left the old binary behind")
	}
}

func TestApplyRefusesAnUnverifiableArchive(t *testing.T) {
	setVersion(t, "2026.08.24")
	st := testStore(t)
	gh := fakeGitHub(t, "2026.08.27", []byte("new"), false)

	exe := filepath.Join(t.TempDir(), config.ExeName("kuro"))
	os.WriteFile(exe, []byte("old"), 0o755)
	u := newUpdater(t, st, gh, exe).WithRestart(func() { t.Error("restarted on an unverified update") })
	u.launch = func(string) error { t.Error("launched an unverified update"); return nil }

	u.Check(context.Background())
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for u.Status().Stage != StageFailed && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if st := u.Status(); st.Stage != StageFailed || st.Error == "" {
		t.Fatalf("status = %+v, want a failure naming the missing checksums", st)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Error("the executable was replaced without verification")
	}
}

// A new binary that can't start (quarantined, say) isn't left with nothing to fall back to.
func TestApplyRestoresTheOldBinaryWhenTheNewOneWontStart(t *testing.T) {
	setVersion(t, "2026.08.24")
	st := testStore(t)
	gh := fakeGitHub(t, "2026.08.27", []byte("new binary"), true)

	exe := filepath.Join(t.TempDir(), config.ExeName("kuro"))
	os.WriteFile(exe, []byte("old binary"), 0o755)
	u := newUpdater(t, st, gh, exe).WithRestart(func() { t.Error("restarted after a failed launch") })
	u.launch = func(string) error { return errors.New("blocked by antivirus") }

	u.Check(context.Background())
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for u.Status().Stage != StageFailed && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Errorf("executable = %q, want the old one restored", got)
	}
}

// The old updater installs the first Yoru as kuro.exe; it takes its new name on start, and a later start leaves it.
func TestTakeNameAfterTheRename(t *testing.T) {
	dir := t.TempDir()
	kuro, yoru := filepath.Join(dir, config.ExeName("kuro")), filepath.Join(dir, config.ExeName("yoru"))
	os.WriteFile(kuro, []byte("new code"), 0o755)

	if got := TakeName(kuro); got != yoru {
		t.Fatalf("now %s, want %s", got, yoru)
	}
	if b, _ := os.ReadFile(yoru); string(b) != "new code" {
		t.Errorf("yoru.exe holds %q", b)
	}
	if _, err := os.Stat(kuro); err == nil {
		t.Error("kuro.exe is still there")
	}
	if got := TakeName(yoru); got != yoru {
		t.Errorf("an already renamed one moved to %s", got)
	}

	// A yoru.exe of the user's own (a fresh download beside the old one) is never replaced.
	os.WriteFile(kuro, []byte("other"), 0o755)
	if got := TakeName(kuro); got != kuro {
		t.Errorf("renamed over an existing yoru.exe: %s", got)
	}
	if b, _ := os.ReadFile(yoru); string(b) != "new code" {
		t.Errorf("yoru.exe was replaced with %q", b)
	}
}

// The first Yoru failing to start hands back to the version before it, which waits as kuro.exe.old.
func TestRollbackAcrossTheRename(t *testing.T) {
	dir := t.TempDir()
	kuro, yoru := filepath.Join(dir, config.ExeName("kuro")), filepath.Join(dir, config.ExeName("yoru"))
	os.WriteFile(yoru, []byte("new, failing"), 0o755)
	os.WriteFile(kuro+".old", []byte("old"), 0o755)

	back, err := Rollback(yoru)
	if err != nil || back != kuro {
		t.Fatalf("rollback to %q (%v), want %s", back, err, kuro)
	}
	if b, _ := os.ReadFile(kuro); string(b) != "old" {
		t.Errorf("kuro.exe holds %q, want the old version", b)
	}
	if b, _ := os.ReadFile(yoru + ".failed"); string(b) != "new, failing" {
		t.Errorf("the failed one kept as %q", b)
	}

	// Serving under the new name later, the kuro-named leftovers go.
	os.WriteFile(yoru, []byte("fixed"), 0o755)
	os.WriteFile(kuro+".old", []byte("old"), 0o755)
	Cleanup(yoru)
	for _, left := range []string{kuro + ".old", yoru + ".failed"} {
		if _, err := os.Stat(left); err == nil {
			t.Errorf("%s left behind", filepath.Base(left))
		}
	}
}

func TestSwapRestoresTheOldBinaryWhenTheNewOneCannotMoveIn(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "kuro.exe")
	os.WriteFile(exe, []byte("old"), 0o755)

	if err := swap(exe, filepath.Join(dir, "missing.new")); err == nil {
		t.Fatal("swap succeeded without a new binary")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Errorf("executable = %q after a failed swap", got)
	}
}

// Two presses at once start one download: the second sees the first's claim.
func TestApplyTwiceStartsOneDownload(t *testing.T) {
	setVersion(t, "2026.08.24")
	st := testStore(t)
	gh := fakeGitHub(t, "2026.08.27", []byte("new binary"), true)
	exe := filepath.Join(t.TempDir(), config.ExeName("kuro"))
	os.WriteFile(exe, []byte("old binary"), 0o755)

	var restarts atomic.Int32
	u := newUpdater(t, st, gh, exe).WithRestart(func() { restarts.Add(1) })
	u.launch = func(string) error { return nil }
	if _, err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { u.Apply() })
	}
	wg.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for u.Status().Stage != StageRestarting && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond)
	if n := restarts.Load(); n != 1 {
		t.Fatalf("%d restarts, want one", n)
	}
}
