package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type config struct {
	Addr          string
	DBPath        string
	BaseURL       string
	GoogleID      string
	GoogleSecret  string
	SessionSecret string
	AdminSub      string
}

func (c config) oauthConfigured() bool { return c.GoogleID != "" && c.GoogleSecret != "" }
func (c config) cookieSecure() bool    { return strings.HasPrefix(c.BaseURL, "https://") }
func (c config) redirectURI() string {
	return strings.TrimSuffix(c.BaseURL, "/") + "/auth/google/callback"
}

// ---------------------------------------------------------------------------
// Sessions: stateless signed cookie (HMAC-SHA256 over "sub|expiry"), 30 days.
// ---------------------------------------------------------------------------

const sessionCookieName = "syh_session"
const sessionLifetime = 30 * 24 * time.Hour

func (a *app) signSession(sub string) string {
	payload := sub + "|" + strconv.FormatInt(time.Now().Add(sessionLifetime).Unix(), 10)
	mac := hmac.New(sha256.New, []byte(a.cfg.SessionSecret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *app) setSessionCookie(w http.ResponseWriter, sub string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    a.signSession(sub),
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   a.cfg.cookieSecure(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *app) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: a.cfg.cookieSecure(), SameSite: http.SameSiteLaxMode,
	})
}

// sessionSub verifies the signed cookie and returns the Google sub, or "".
func (a *app) sessionSub(r *http.Request) string {
	ck, err := r.Cookie(sessionCookieName)
	if err != nil || ck.Value == "" {
		return ""
	}
	parts := strings.Split(ck.Value, ".")
	if len(parts) != 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return ""
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(a.cfg.SessionSecret))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return ""
	}
	kv := strings.Split(string(payload), "|")
	if len(kv) != 2 {
		return ""
	}
	exp, err := strconv.ParseInt(kv[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return ""
	}
	return kv[0]
}

// currentUser resolves the session cookie to a user row (nil when logged out).
func (a *app) currentUser(r *http.Request) *user {
	sub := a.sessionSub(r)
	if sub == "" {
		return nil
	}
	u, err := getUserBySub(a.db, sub)
	if err != nil {
		return nil
	}
	return u
}

// signValue produces a tamper-proof token for the name-confirmation step.
func (a *app) signValue(v string) string {
	mac := hmac.New(sha256.New, []byte(a.cfg.SessionSecret))
	mac.Write([]byte("sign:" + v))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *app) verifyValue(v, sig string) bool {
	return hmac.Equal([]byte(a.signValue(v)), []byte(sig))
}

// ---------------------------------------------------------------------------
// Tiny in-memory per-IP rate limiter (auth + name setup).
// ---------------------------------------------------------------------------

type rateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newRateLimiter() *rateLimiter { return &rateLimiter{hits: map[string][]time.Time{}} }

func (rl *rateLimiter) allow(ip string, limit int, window time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cut := now.Add(-window)
	kept := rl.hits[ip][:0]
	for _, t := range rl.hits[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		rl.hits[ip] = kept
		return false
	}
	rl.hits[ip] = append(kept, now)
	return true
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------------------------------------------------------------------------
// Google OAuth (implemented directly against Google's endpoints; no extra deps).
// ---------------------------------------------------------------------------

const oauthStateCookie = "syh_oauth_state"
const refCookieName = "syh_ref"
const anonCookieName = "syh_anon"

func (a *app) handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.allow(clientIP(r), 30, 10*time.Minute) {
		http.Error(w, tr(langOf(r), "err_too_many"), http.StatusTooManyRequests)
		return
	}
	if !a.creds.configured() {
		http.Error(w, tr(langOf(r), "err_not_config"), http.StatusServiceUnavailable)
		return
	}
	googleID, _ := a.creds.get()
	state := randomHex(16)
	http.SetCookie(w, &http.Cookie{
		Name: oauthStateCookie, Value: state, Path: "/", MaxAge: 600,
		HttpOnly: true, Secure: a.cfg.cookieSecure(), SameSite: http.SameSiteLaxMode,
	})
	q := url.Values{
		"client_id":     {googleID},
		"redirect_uri":  {a.cfg.redirectURI()},
		"response_type": {"code"},
		"scope":         {"openid profile"},
		"state":         {state},
		"prompt":        {"select_account"},
	}
	http.Redirect(w, r, "https://accounts.google.com/o/oauth2/v2/auth?"+q.Encode(), http.StatusFound)
}

func (a *app) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.allow(clientIP(r), 30, 10*time.Minute) {
		http.Error(w, tr(langOf(r), "err_too_many"), http.StatusTooManyRequests)
		return
	}
	if !a.creds.configured() {
		http.Error(w, tr(langOf(r), "err_not_config"), http.StatusServiceUnavailable)
		return
	}
	googleID, googleSecret := a.creds.get()
	stateCk, err := r.Cookie(oauthStateCookie)
	if err != nil || stateCk.Value == "" || stateCk.Value != r.URL.Query().Get("state") {
		http.Error(w, tr(langOf(r), "err_signin_expired"), http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, tr(langOf(r), "err_signin_cancelled"), http.StatusBadRequest)
		return
	}
	// Exchange the code for tokens.
	form := url.Values{
		"code":          {code},
		"client_id":     {googleID},
		"client_secret": {googleSecret},
		"redirect_uri":  {a.cfg.redirectURI()},
		"grant_type":    {"authorization_code"},
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm("https://oauth2.googleapis.com/token", form)
	if err != nil {
		http.Error(w, tr(langOf(r), "err_reach_google"), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		http.Error(w, tr(langOf(r), "err_google_failed"), http.StatusBadGateway)
		return
	}
	// Fetch the stable user id (sub) from userinfo.
	req, _ := http.NewRequest("GET", "https://openidconnect.googleapis.com/v1/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	uresp, err := client.Do(req)
	if err != nil {
		http.Error(w, tr(langOf(r), "err_reach_google"), http.StatusBadGateway)
		return
	}
	defer uresp.Body.Close()
	var info struct {
		Sub string `json:"sub"`
	}
	if err := json.NewDecoder(uresp.Body).Decode(&info); err != nil || info.Sub == "" {
		http.Error(w, tr(langOf(r), "err_google_failed"), http.StatusBadGateway)
		return
	}

	// Create the user row on first login; remember who referred them.
	isNew := false
	u, err := getUserBySub(a.db, info.Sub)
	if err != nil {
		http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
		return
	}
	if u == nil {
		isNew = true
		if _, err := a.db.Exec(`INSERT INTO users(google_sub, ref_code, created_at) VALUES(?,?,?)`,
			info.Sub, randomHex(5), time.Now().Unix()); err != nil {
			http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
			return
		}
		u, err = getUserBySub(a.db, info.Sub)
		if err != nil || u == nil {
			http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
			return
		}
		if refCk, err := r.Cookie(refCookieName); err == nil && refCk.Value != "" {
			var referrerID int64
			err := a.db.QueryRow(`SELECT id FROM users WHERE ref_code=?`, refCk.Value).Scan(&referrerID)
			if err == nil && referrerID != u.ID {
				_, _ = a.db.Exec(`INSERT OR IGNORE INTO referrals(invitee_id, referrer_id) VALUES(?,?)`,
					u.ID, referrerID)
			}
		}
	}
	_ = isNew
	a.setSessionCookie(w, info.Sub)
	if u.Name == "" {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}
