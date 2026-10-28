package server

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"yoru/internal/config"
)

var shownCode = regexp.MustCompile(`class="code">([A-Z2-9]{3})-([A-Z2-9]{3})<`)

const tvAgent = "Mozilla/5.0 (Linux; Android 9; BRAVIA 4K) AppleWebKit/537.36 Chrome/108.0 Safari/537.36"

// pairingCode opens Yoru on a TV without the token and reads the code it is shown.
func pairingCode(t *testing.T, tv *device) string {
	t.Helper()
	res := tv.get("/", true)
	page, _ := io.ReadAll(res.Body)
	m := shownCode.FindStringSubmatch(string(page))
	if res.StatusCode != http.StatusUnauthorized || m == nil {
		t.Fatalf("a TV without the token: %d %.200s", res.StatusCode, page)
	}
	return m[1] + m[2]
}

func waitStatus(t *testing.T, tv *device) string {
	t.Helper()
	var out struct {
		Status string `json:"status"`
	}
	json.NewDecoder(tv.get("/api/pair/wait", false).Body).Decode(&out)
	return out.Status
}

// A TV shown a code is let in once a device with the token enters it, typed any way; until then it isn't.
func TestATVPairsWithACode(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	h.server.token = "s3cret"
	tv := newDevice(t, h)
	tv.ua, tv.addr = tvAgent, "192.168.1.60:5555"

	code := pairingCode(t, tv)
	if again := pairingCode(t, tv); again != code {
		t.Errorf("a reload showed another code: %s, then %s", code, again)
	}
	if s := waitStatus(t, tv); s != "waiting" {
		t.Errorf("before the code is entered: %q", s)
	}
	if res := tv.get("/api/health", false); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("the TV got in before pairing: %d", res.StatusCode)
	}

	phone := newDevice(t, h)
	phone.get("/api/health?token=s3cret", false)
	if res := phone.do("POST", "/api/pair", `{"code":"ZZZ-ZZZ"}`, false); res.StatusCode != http.StatusNotFound {
		t.Errorf("a code nobody shows: %d", res.StatusCode)
	}
	typed := strings.ToLower(code[:3] + " " + code[3:])
	res := phone.do("POST", "/api/pair", `{"code":"`+typed+`"}`, false)
	var paired struct {
		Paired string `json:"paired"`
	}
	json.NewDecoder(res.Body).Decode(&paired)
	if res.StatusCode != http.StatusOK || paired.Paired != "Chrome on Android" {
		t.Fatalf("entering %q: %d %+v", typed, res.StatusCode, paired)
	}

	if s := waitStatus(t, tv); s != "approved" {
		t.Fatalf("after the code: %q", s)
	}
	if res := tv.get("/api/health", false); res.StatusCode != http.StatusOK {
		t.Errorf("the paired TV is still refused: %d", res.StatusCode)
	}
}

// With device approval on only the host pairs, and the TV it pairs is accepted, not asked to wait again.
func TestPairingWithApprovalOnIsTheHosts(t *testing.T) {
	h := approvalHarness(t, approvalOnce)
	tv := newDevice(t, h)
	tv.ua, tv.addr = tvAgent, "192.168.1.60:5555"
	code := pairingCode(t, tv)

	phone := newDevice(t, h)
	phone.get("/api/health?token=s3cret", false)
	if res := phone.do("POST", "/api/pair", `{"code":"`+code+`"}`, false); res.StatusCode == http.StatusOK {
		t.Error("a device other than the host paired with approval on")
	}
	if rec := h.host(t, "POST", "/api/pair", `{"code":"`+code+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("the host entering the code: %d %s", rec.Code, rec.Body)
	}
	if s := waitStatus(t, tv); s != "approved" {
		t.Fatalf("after the host's code: %q", s)
	}
	if res := tv.get("/api/health", false); res.StatusCode != http.StatusOK {
		t.Errorf("the paired TV was made to wait for approval: %d", res.StatusCode)
	}
	approved := false
	for _, d := range h.devices(t) {
		approved = approved || d["status"] == "approved"
	}
	if !approved {
		t.Errorf("the paired TV isn't among the accepted devices: %v", h.devices(t))
	}
}

// Pages fetched without keeping cookies (a scanner, a link preview) take their own address's 3 places,
// leaving a TV elsewhere its code.
func TestPairingIsLimitedPerAddress(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	h.server.token = "s3cret"
	scanner := func() *device {
		d := newDevice(t, h)
		d.addr = "203.0.113.9:4000"
		return d
	}
	for range pairPerAddr {
		pairingCode(t, scanner())
	}
	page, _ := io.ReadAll(scanner().get("/", true).Body)
	if !strings.Contains(string(page), "Too many devices are waiting") {
		t.Errorf("a 4th from one address: %.200s", page)
	}
	tv := newDevice(t, h)
	tv.ua, tv.addr = tvAgent, "192.168.1.60:5555"
	pairingCode(t, tv)
}

// Codes last 10 minutes, and no more than 20 wait at once.
func TestPairingCodesExpireAndAreLimited(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	h.server.token = "s3cret"
	tv := newDevice(t, h)
	code := pairingCode(t, tv)
	h.server.pairMu.Lock()
	for _, p := range h.server.pairs {
		p.created = time.Now().Add(-11 * time.Minute)
	}
	h.server.pairMu.Unlock()
	if s := waitStatus(t, tv); s != "expired" {
		t.Errorf("an 11-minute-old code: %q", s)
	}
	if rec := h.host(t, "POST", "/api/pair", `{"code":"`+code+`"}`); rec.Code != http.StatusNotFound {
		t.Errorf("an expired code was taken: %d", rec.Code)
	}

	for i := range maxPairing {
		d := newDevice(t, h)
		d.addr = "192.168.1." + strconv.Itoa(100+i) + ":5555"
		pairingCode(t, d)
	}
	res := newDevice(t, h).get("/", true)
	page, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(page), "Too many devices are waiting") || shownCode.Match(page) {
		t.Errorf("past %d waiting: %.200s", maxPairing, page)
	}
}
