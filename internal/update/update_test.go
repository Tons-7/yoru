package update

import (
	"archive/zip"
	"bytes"
	"cmp"
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
	"path"
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

// --no-window is forwarded, never added, so a windowed Yoru reopens its UI.
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
	cmd := relaunchCmd(filepath.Join(t.TempDir(), config.ExeName("yoru")))

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
	return fakeRelease(t, version, exeBody, sums, trusted, false)
}

// fakeRelease also serves github.com's own pages; limited, the API says its hourly allowance is spent.
func fakeRelease(t *testing.T, version string, exeBody []byte, sums, trusted, limited bool) *httptest.Server {
	t.Helper()
	return fakeTamperedRelease(t, version, exeBody, sums, trusted, limited, tamper{})
}

// tamper is what a broken mirror or a mis-listed release does to the files the signed sums vouch for.
type tamper struct {
	listedAs string // the name the signed sums give the zip; "" is its own
	served   []byte // a zip served in place of the one the sums hash
}

func fakeTamperedRelease(t *testing.T, version string, exeBody []byte, sums, trusted, limited bool, tm tamper) *httptest.Server {
	t.Helper()

	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	f, _ := zw.Create(config.ExeName(config.Name))
	f.Write(exeBody)
	zw.Close()
	sum := sha256.Sum256(archive.Bytes())
	listed := cmp.Or(tm.listedAs, AssetName(version))
	lines := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), listed)
	served := archive.Bytes()
	if tm.served != nil {
		served = tm.served
	}

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
		if limited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprint(resetAt.Unix()))
			http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
			return
		}
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
		w.Write(served)
	})
	mux.HandleFunc("/dl/"+sumsFile, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, lines)
	})
	mux.HandleFunc("/dl/"+sigFile, func(w http.ResponseWriter, r *http.Request) {
		w.Write(sig)
	})
	site := "/" + Repo + "/releases/"
	mux.HandleFunc(site+"latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+site+"tag/v"+version, http.StatusFound)
	})
	files := map[string][]byte{AssetName(version): served}
	if sums {
		files[sumsFile], files[sigFile] = []byte(lines), sig
	}
	mux.HandleFunc(site+"download/v"+version+"/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[path.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A release signed by any other key is not Yoru's, whatever its checksums say.
func TestApplyRefusesAReleaseSignedByAStranger(t *testing.T) {
	setVersion(t, "2026.08.24")
	st := testStore(t)
	gh := fakeGitHubSigned(t, "2026.08.27", []byte("new binary"), true, false)
	exe := filepath.Join(t.TempDir(), config.ExeName("yoru"))
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

// A signed sums file vouches for one zip: a different one, or none listed for this platform, installs nothing.
func TestApplyRefusesAnArchiveItsSignedSumsDontVouchFor(t *testing.T) {
	for _, c := range []struct {
		name string
		tm   tamper
		want string
	}{
		{"mismatch", tamper{served: []byte("PK not the zip that was hashed")}, "checksum mismatch"},
		{"unlisted", tamper{listedAs: "yoru-2026.08.27-plan9-update.zip"}, "no entry"},
	} {
		t.Run(c.name, func(t *testing.T) {
			setVersion(t, "2026.08.24")
			st := testStore(t)
			gh := fakeTamperedRelease(t, "2026.08.27", []byte("new binary"), true, true, false, c.tm)
			exe := filepath.Join(t.TempDir(), config.ExeName("yoru"))
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
			if s := u.Status(); s.Stage != StageFailed || !strings.Contains(s.Error, c.want) {
				t.Fatalf("status = %+v, want it refused (%q)", s, c.want)
			}
			if got, _ := os.ReadFile(exe); string(got) != "old binary" {
				t.Fatal("the executable was replaced")
			}
		})
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
	u.api, u.web = gh.URL, gh.URL
	return u
}

// When the limited API lets this network ask again.
var resetAt = time.Date(2026, 10, 18, 19, 42, 0, 0, time.Local)

// Behind a VPN the API's hourly allowance is soon spent; github.com's own pages still say what's out.
func TestRateLimitedCheckReadsTheSite(t *testing.T) {
	setVersion(t, "2026.08.24")
	st := testStore(t)
	gh := fakeRelease(t, "2026.08.27", []byte("new binary"), true, true, true)
	exe := filepath.Join(t.TempDir(), config.ExeName("yoru"))
	os.WriteFile(exe, []byte("old binary"), 0o755)
	restarted := make(chan struct{})
	u := newUpdater(t, st, gh, exe).WithRestart(func() { close(restarted) })
	u.launch = func(string) error { return nil }

	got, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Available || got.Latest == nil || got.Latest.Version != "2026.08.27" || got.CheckErr != "" {
		t.Fatalf("status = %+v, want 2026.08.27 available", got)
	}
	if len(got.Pending) != 1 || got.Pending[0].Version != "2026.08.27" {
		t.Errorf("pending = %+v, want the new version listed", got.Pending)
	}
	// Signed and checked as any other: the site gives the same files.
	if err := u.Apply(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-restarted:
	case <-time.After(10 * time.Second):
		t.Fatalf("no restart; status = %+v", u.Status())
	}
	if b, _ := os.ReadFile(exe); string(b) != "new binary" {
		t.Errorf("executable = %q, want the new one", b)
	}
}

// Notes from an earlier check stay; the site has none to give.
func TestRateLimitedCheckKeepsTheNotesItHad(t *testing.T) {
	setVersion(t, "2026.08.24")
	u := newUpdater(t, testStore(t), fakeRelease(t, "2026.08.27", []byte("new"), true, true, true), "yoru.exe")
	u.set(func(s *Status) { s.Pending = []Notes{{Version: "2026.08.27", Notes: "Faster seeks"}} })
	got, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pending) != 1 || got.Pending[0].Notes != "Faster seeks" {
		t.Errorf("pending = %+v, want the notes read before", got.Pending)
	}

	// Up to date, the site's answer adds nothing blank to What's new.
	setVersion(t, "2026.08.27")
	u.set(func(s *Status) { s.Pending, s.WhatsNew = nil, nil })
	if got, _ = u.Check(context.Background()); len(got.WhatsNew) != 0 {
		t.Errorf("what's new = %+v, want nothing made up for the version running", got.WhatsNew)
	}
}

// Limited, the notes come from the release feed, which the API's limit doesn't count: What's new after an
// update isn't left empty. Its HTML goes back to the headings, bullets and bold the About tab reads.
func TestRateLimitedCheckReadsTheNotesFromTheFeed(t *testing.T) {
	setVersion(t, "2026.08.27")
	gh := fakeRelease(t, "2026.08.27", []byte("new"), true, true, true)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+Repo+"/releases.atom" {
			gh.Config.Handler.ServeHTTP(w, r)
			return
		}
		io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom"><entry>`+
			`<link rel="alternate" type="text/html" href="https://github.com/`+Repo+`/releases/tag/v2026.08.27"/>`+
			`<content type="html">&lt;h2&gt;New&lt;/h2&gt;&#10;&lt;ul&gt;&#10;&lt;li&gt;&lt;strong&gt;Faster seeks:&lt;/strong&gt; `+
			`Yoru&#39;s own &lt;a href="x"&gt;engine&lt;/a&gt;&lt;/li&gt;&#10;&lt;li&gt;Two&lt;/li&gt;&#10;&lt;/ul&gt;</content></entry></feed>`)
	}))
	t.Cleanup(site.Close)

	got, err := newUpdater(t, testStore(t), site, "yoru.exe").Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "## New\n\n- **Faster seeks:** Yoru's own engine\n- Two"
	if len(got.WhatsNew) != 1 || got.WhatsNew[0].Notes != want {
		t.Errorf("what's new = %+v, want %q", got.WhatsNew, want)
	}
}

// With the site out of reach too, the check says what's wrong and when it clears, not "HTTP 403".
func TestRateLimitedCheckSaysUntilWhen(t *testing.T) {
	setVersion(t, "2026.08.24")
	gh := fakeRelease(t, "2026.08.27", []byte("new"), true, true, true)
	u := newUpdater(t, testStore(t), gh, "yoru.exe")
	u.web = gh.URL + "/elsewhere"
	_, err := u.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "limiting update checks from this network until 19:42") {
		t.Fatalf("err = %v, want it to say GitHub limits checks until 19:42", err)
	}
	if st := u.Status(); !strings.Contains(st.CheckErr, "until 19:42") {
		t.Errorf("checkError = %q", st.CheckErr)
	}

	// A refusal that isn't the allowance is no limit.
	res := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"X-Ratelimit-Remaining": {"12"}}}
	if limitOf(res) != nil {
		t.Error("a 403 with allowance left was read as a rate limit")
	}
}

func TestCheckNotifiesOncePerVersion(t *testing.T) {
	setVersion(t, "2026.08.24")
	st := testStore(t)
	gh := fakeGitHub(t, "2026.08.27", []byte("new"), true)
	u := newUpdater(t, st, gh, filepath.Join(t.TempDir(), "yoru.exe"))
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
	u := newUpdater(t, st, gh, filepath.Join(t.TempDir(), "yoru.exe"))

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

	exe := filepath.Join(t.TempDir(), config.ExeName("yoru"))
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

	exe := filepath.Join(t.TempDir(), config.ExeName("yoru"))
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

	exe := filepath.Join(t.TempDir(), config.ExeName("yoru"))
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

// A new version failing to start hands back to the one kept as .old; once one serves, the leftovers go.
func TestRollbackThenCleanup(t *testing.T) {
	exe := filepath.Join(t.TempDir(), config.ExeName("yoru"))
	os.WriteFile(exe, []byte("new, failing"), 0o755)
	os.WriteFile(exe+".old", []byte("old"), 0o755)

	if err := Rollback(exe); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Errorf("yoru.exe holds %q, want the old version", b)
	}
	if b, _ := os.ReadFile(exe + ".failed"); string(b) != "new, failing" {
		t.Errorf("the failed one kept as %q", b)
	}
	Cleanup(exe)
	if _, err := os.Stat(exe + ".failed"); err == nil {
		t.Error("yoru.exe.failed left behind")
	}
}

func TestSwapRestoresTheOldBinaryWhenTheNewOneCannotMoveIn(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "yoru.exe")
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
	exe := filepath.Join(t.TempDir(), config.ExeName("yoru"))
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
