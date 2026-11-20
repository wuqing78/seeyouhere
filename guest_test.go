package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) *app {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &app{
		db:      db,
		cfg:     config{SessionSecret: "test-secret", BaseURL: "http://example.test"},
		hub:     newHub(),
		limiter: newRateLimiter(),
	}
}

func guestEnter(t *testing.T, a *app, ip string) (string, *user) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/guest", nil)
	req.RemoteAddr = ip + ":1234"
	rec := httptest.NewRecorder()
	a.handleGuestEnter(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("guest enter: status %d, body %s", rec.Code, rec.Body.String())
	}
	var cookie string
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("guest enter: no session cookie set")
	}
	sub := a.sessionSub(&http.Request{Header: http.Header{"Cookie": {sessionCookieName + "=" + cookie}}})
	u, err := getUserBySub(a.db, sub)
	if err != nil || u == nil {
		t.Fatalf("guest enter: user lookup failed (%v, %v)", u, err)
	}
	return cookie, u
}

func asUser(a *app, cookie string, method, path string, body string) *httptest.ResponseRecorder {
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
	rec := httptest.NewRecorder()
	switch path {
	case "/checkin":
		a.handleCheckin(rec, req)
	case "/tap":
		a.handleTap(rec, req)
	}
	return rec
}

func addRegistered(t *testing.T, a *app, sub, name string) (string, *user) {
	t.Helper()
	if _, err := a.db.Exec(`INSERT INTO users(google_sub, name, name_key, ref_code, created_at) VALUES(?,?,?,?,?)`,
		sub, name, nameKeyOf(name), randomHex(5), time.Now().Unix()); err != nil {
		t.Fatalf("addRegistered: %v", err)
	}
	u, err := getUserBySub(a.db, sub)
	if err != nil || u == nil {
		t.Fatalf("addRegistered lookup: %v", err)
	}
	return a.signSession(sub), u
}

func pointsOf(t *testing.T, a *app, id int64) int64 {
	t.Helper()
	var p int64
	if err := a.db.QueryRow(`SELECT points FROM users WHERE id=?`, id).Scan(&p); err != nil {
		t.Fatalf("pointsOf: %v", err)
	}
	return p
}

func tapCount(t *testing.T, a *app, from, to int64) int64 {
	t.Helper()
	var c int64
	err := a.db.QueryRow(`SELECT count FROM taps WHERE day=? AND from_id=? AND to_id=?`,
		dayKey(time.Now()), from, to).Scan(&c)
	if err != nil {
		return 0
	}
	return c
}

func TestGuestMode(t *testing.T) {
	a := testApp(t)

	g1Cookie, g1 := guestEnter(t, a, "10.0.0.1")
	if g1.Name != "Guest 1" || !isGuestSub(g1.Sub) {
		t.Fatalf("guest 1 = %+v, want name Guest 1 with guest sub", g1)
	}
	_, g2 := guestEnter(t, a, "10.0.0.2")
	if g2.Name != "Guest 2" {
		t.Fatalf("guest 2 name = %q, want Guest 2", g2.Name)
	}
	g2Cookie := a.signSession(g2.Sub)

	r1Cookie, r1 := addRegistered(t, a, "reg:1", "RegOne")
	_, r2 := addRegistered(t, a, "reg:2", "RegTwo")
	r2Cookie := a.signSession(r2.Sub)

	for name, ck := range map[string]string{"g1": g1Cookie, "g2": g2Cookie, "r1": r1Cookie, "r2": r2Cookie} {
		if rec := asUser(a, ck, http.MethodPost, "/checkin", ""); rec.Code != http.StatusFound {
			t.Fatalf("checkin %s: status %d", name, rec.Code)
		}
	}

	tap := func(cookie string, to int64) tapResponse {
		rec := asUser(a, cookie, http.MethodPost, "/tap", `{"to":`+strconv.FormatInt(to, 10)+`}`)
		var resp tapResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("tap response not JSON: %v (%s)", err, rec.Body.String())
		}
		return resp
	}

	// Guest taps guest: signal recorded, no points.
	if resp := tap(g1Cookie, g2.ID); !resp.OK {
		t.Fatalf("guest->guest tap failed: %+v", resp)
	}
	if got := pointsOf(t, a, g2.ID); got != 0 {
		t.Fatalf("guest points after being tapped = %d, want 0", got)
	}
	if got := tapCount(t, a, g1.ID, g2.ID); got != 1 {
		t.Fatalf("guest->guest tap count = %d, want 1", got)
	}

	// Guest taps registered user: signal recorded, recipient gains NO point.
	if resp := tap(g1Cookie, r1.ID); !resp.OK {
		t.Fatalf("guest->registered tap failed: %+v", resp)
	}
	if got := pointsOf(t, a, r1.ID); got != 0 {
		t.Fatalf("registered points after guest tap = %d, want 0", got)
	}
	if got := tapCount(t, a, g1.ID, r1.ID); got != 1 {
		t.Fatalf("guest->registered tap count = %d, want 1", got)
	}

	// Registered taps guest: guest gains nothing.
	if resp := tap(r1Cookie, g2.ID); !resp.OK {
		t.Fatalf("registered->guest tap failed: %+v", resp)
	}
	if got := pointsOf(t, a, g2.ID); got != 0 {
		t.Fatalf("guest points after registered tap = %d, want 0", got)
	}

	// Registered taps registered: the point still moves.
	if resp := tap(r1Cookie, r2.ID); !resp.OK {
		t.Fatalf("registered->registered tap failed: %+v", resp)
	}
	if got := pointsOf(t, a, r2.ID); got != 1 {
		t.Fatalf("registered points after registered tap = %d, want 1", got)
	}

	// Guest quota is spent like anyone's: g1 tapped twice from 24.
	resp := tap(g1Cookie, r1.ID)
	if !resp.OK || resp.QuotaLeft != 21 {
		t.Fatalf("guest third tap: ok=%v quotaLeft=%d, want ok and 21", resp.OK, resp.QuotaLeft)
	}
}
