package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type app struct {
	db      *sql.DB
	tmpls   map[string]*template.Template
	cfg     config
	creds   *credState
	hub     *hub
	limiter *rateLimiter
}

// render executes the named template in the language the request asks for
// (syh_lang cookie; English by default).
func (a *app) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tmpls[langOf(r)].ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
	}
}

// ---------------------------------------------------------------------------
// Home: window view (not checked in) or inside view (checked in).
// ---------------------------------------------------------------------------

type plate struct {
	ID       int64
	Name     string
	Me       bool
	TappedMe int64 // how many times this person tapped me today
	ITapped  bool  // I tapped them today (amber accent)
}

type homeData struct {
	LoggedIn  bool
	Banned    bool
	Inside    bool
	IsAdmin   bool
	DateLabel string
	TotalHere int

	// window view
	WindowPlates []plate
	MoreCount    int

	// inside view + identity footer
	MeName         string
	Plates         []plate
	Days           int
	Level          int
	QuotaTotal     int
	QuotaLeft      int
	MeterPct       int
	Tickets        int64
	Points         int64
	TappedYouToday int64
	RefLink        string
}

var weekdaysZH = []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

func dayLabel(lang, day string) string {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	if lang == "zh" {
		return fmt.Sprintf("%d月%d日 %s", int(t.Month()), t.Day(), weekdaysZH[int(t.Weekday())])
	}
	return t.Format("Mon, Jan 2")
}

func (a *app) roster(day string) ([]plate, error) {
	rows, err := a.db.Query(`SELECT u.id, u.name FROM attendance att
		JOIN users u ON u.id = att.user_id
		WHERE att.day=? AND u.banned=0 AND u.name<>''
		ORDER BY att.checked_at, u.id`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []plate
	for rows.Next() {
		var p plate
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Remember a referral code across the sign-in flow.
	if ref := r.URL.Query().Get("ref"); ref != "" && len(ref) <= 32 && isAlnum(ref) {
		http.SetCookie(w, &http.Cookie{
			Name: refCookieName, Value: ref, Path: "/", MaxAge: 30 * 24 * 3600,
			HttpOnly: true, Secure: a.cfg.cookieSecure(), SameSite: http.SameSiteLaxMode,
		})
	}
	day := dayKey(time.Now())
	u := a.currentUser(r)
	if u != nil && u.Name == "" {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	data := homeData{DateLabel: dayLabel(langOf(r), day), LoggedIn: u != nil}
	roster, err := a.roster(day)
	if err != nil {
		http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
		return
	}
	data.TotalHere = len(roster)

	if u != nil {
		data.MeName = u.Name
		data.Points = u.Points
		data.Tickets = u.Tickets
		data.IsAdmin = a.cfg.AdminSub != "" && u.Sub == a.cfg.AdminSub
		data.RefLink = a.cfg.BaseURL + "/?ref=" + u.RefCode
		days, _ := attendanceDays(a.db, u.ID)
		data.Days = days
		data.Level = levelForDays(days)
		if u.Banned {
			data.Banned = true
			a.render(w, r, "home", data)
			return
		}
		inside, _ := checkedIn(a.db, u.ID, day)
		if inside {
			data.Inside = true
			data.QuotaTotal = dailyQuota(days)
			var used int
			_ = a.db.QueryRow(`SELECT quota_used FROM daily_spend WHERE user_id=? AND day=?`,
				u.ID, day).Scan(&used)
			data.QuotaLeft = data.QuotaTotal - used
			if data.QuotaLeft < 0 {
				data.QuotaLeft = 0
			}
			if data.QuotaTotal > 0 {
				data.MeterPct = data.QuotaLeft * 100 / data.QuotaTotal
			}
			// Who tapped me today, and whom did I tap.
			tappedMe := map[int64]int64{}
			rows, err := a.db.Query(`SELECT from_id, count FROM taps WHERE day=? AND to_id=?`, day, u.ID)
			if err == nil {
				for rows.Next() {
					var from, cnt int64
					if err := rows.Scan(&from, &cnt); err == nil {
						tappedMe[from] = cnt
						data.TappedYouToday += cnt
					}
				}
				rows.Close()
			}
			iTapped := map[int64]bool{}
			rows2, err := a.db.Query(`SELECT to_id FROM taps WHERE day=? AND from_id=?`, day, u.ID)
			if err == nil {
				for rows2.Next() {
					var to int64
					if err := rows2.Scan(&to); err == nil {
						iTapped[to] = true
					}
				}
				rows2.Close()
			}
			for _, p := range roster {
				p.Me = p.ID == u.ID
				p.TappedMe = tappedMe[p.ID]
				p.ITapped = iTapped[p.ID]
				data.Plates = append(data.Plates, p)
			}
			a.render(w, r, "home", data)
			return
		}
	}

	// Window view: deterministic per-viewer sample of the roster.
	viewerKey := ""
	if u != nil {
		viewerKey = "u:" + strconv.FormatInt(u.ID, 10)
	} else {
		anon := ""
		if ck, err := r.Cookie(anonCookieName); err == nil {
			anon = ck.Value
		}
		if anon == "" {
			anon = randomHex(8)
			http.SetCookie(w, &http.Cookie{
				Name: anonCookieName, Value: anon, Path: "/", MaxAge: 365 * 24 * 3600,
				HttpOnly: true, Secure: a.cfg.cookieSecure(), SameSite: http.SameSiteLaxMode,
			})
		}
		viewerKey = "a:" + anon
	}
	ids := make([]int64, len(roster))
	byID := map[int64]plate{}
	for i, p := range roster {
		ids[i] = p.ID
		byID[p.ID] = p
	}
	for _, id := range sampleWindow(ids, viewerKey, day) {
		data.WindowPlates = append(data.WindowPlates, byID[id])
	}
	data.MoreCount = data.TotalHere - len(data.WindowPlates)
	if data.MoreCount < 0 {
		data.MoreCount = 0
	}
	a.render(w, r, "home", data)
}

func isAlnum(s string) bool {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return len(s) > 0
}

// ---------------------------------------------------------------------------
// Name setup: choose (step 1) -> confirm, warned it can never change (step 2).
// ---------------------------------------------------------------------------

type setupData struct {
	Error string
	Name  string
	Sig   string
}

func (a *app) handleSetup(w http.ResponseWriter, r *http.Request) {
	u := a.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/auth/google/login", http.StatusFound)
		return
	}
	if u.Name != "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if r.Method == http.MethodPost {
		if !a.limiter.allow(clientIP(r), 30, 10*time.Minute) {
			http.Error(w, tr(langOf(r), "err_too_many"), http.StatusTooManyRequests)
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		if msg := validateName(name); msg != "" {
			a.render(w, r, "setup", setupData{Error: msgByEnglish(langOf(r), msg), Name: name})
			return
		}
		var taken int
		_ = a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE name_key=?`, nameKeyOf(name)).Scan(&taken)
		if taken > 0 {
			a.render(w, r, "setup", setupData{Error: tr(langOf(r), "msg_taken"), Name: name})
			return
		}
		a.render(w, r, "setupconfirm", setupData{Name: name, Sig: a.signValue(u.Sub + "|" + name)})
		return
	}
	a.render(w, r, "setup", setupData{})
}

func (a *app) handleSetupConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	u := a.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/auth/google/login", http.StatusFound)
		return
	}
	if u.Name != "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if !a.limiter.allow(clientIP(r), 30, 10*time.Minute) {
		http.Error(w, tr(langOf(r), "err_too_many"), http.StatusTooManyRequests)
		return
	}
	name := r.FormValue("name")
	if !a.verifyValue(u.Sub+"|"+name, r.FormValue("sig")) || validateName(name) != "" {
		a.render(w, r, "setup", setupData{Error: tr(langOf(r), "msg_went_wrong"), Name: name})
		return
	}
	res, err := a.db.Exec(`UPDATE users SET name=?, name_key=? WHERE google_sub=? AND name=''`,
		name, nameKeyOf(name), u.Sub)
	if err != nil { // most likely the unique name_key index: lost a race
		a.render(w, r, "setup", setupData{Error: tr(langOf(r), "msg_just_taken"), Name: name})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// ---------------------------------------------------------------------------
// Check-in ("I'm here"): puts you on today's roster, grants today's quota.
// Irreversible for the day. A referred user's FIRST EVER check-in earns the
// referrer +1 referral ticket.
// ---------------------------------------------------------------------------

func (a *app) handleCheckin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	u := a.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/auth/google/login", http.StatusFound)
		return
	}
	if u.Name == "" {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	if u.Banned {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	day := dayKey(time.Now())
	tx, err := a.db.Begin()
	if err != nil {
		http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	var priorDays int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM attendance WHERE user_id=?`, u.ID).Scan(&priorDays); err != nil {
		http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
		return
	}
	res, err := tx.Exec(`INSERT OR IGNORE INTO attendance(user_id, day, checked_at) VALUES(?,?,?)`,
		u.ID, day, time.Now().Unix())
	if err != nil {
		http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 && priorDays == 0 {
		// First check-in ever: pay the referral ticket, once.
		var referrerID int64
		err := tx.QueryRow(`SELECT referrer_id FROM referrals WHERE invitee_id=? AND rewarded_at IS NULL`,
			u.ID).Scan(&referrerID)
		if err == nil {
			if _, err := tx.Exec(`UPDATE users SET tickets=tickets+1 WHERE id=?`, referrerID); err != nil {
				http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
				return
			}
			if _, err := tx.Exec(`UPDATE referrals SET rewarded_at=? WHERE invitee_id=?`,
				time.Now().Unix(), u.ID); err != nil {
				http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// ---------------------------------------------------------------------------
// Tap: one tap costs 1 from today's quota first, then referral tickets.
// Fully transactional: spend + pair count + recipient points move together.
// ---------------------------------------------------------------------------

type tapRequest struct {
	To int64 `json:"to"`
}

type tapResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	QuotaLeft int    `json:"quotaLeft"`
	Tickets   int64  `json:"tickets"`
	MyCount   int64  `json:"myCount"` // times I have tapped them today
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *app) handleTap(w http.ResponseWriter, r *http.Request) {
	lang := langOf(r)
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, tapResponse{Error: tr(lang, "tap_post_only")})
		return
	}
	u := a.currentUser(r)
	if u == nil || u.Name == "" {
		writeJSON(w, http.StatusUnauthorized, tapResponse{Error: tr(lang, "err_signin_first")})
		return
	}
	if u.Banned {
		writeJSON(w, http.StatusForbidden, tapResponse{Error: tr(lang, "err_suspended")})
		return
	}
	var req tapRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.To <= 0 {
		writeJSON(w, http.StatusBadRequest, tapResponse{Error: tr(lang, "err_bad_request")})
		return
	}
	if req.To == u.ID {
		writeJSON(w, http.StatusBadRequest, tapResponse{Error: tr(lang, "err_self_tap")})
		return
	}
	day := dayKey(time.Now())
	tx, err := a.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
		return
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM attendance WHERE user_id=? AND day=?`, u.ID, day).Scan(&n); err != nil || n == 0 {
		writeJSON(w, http.StatusForbidden, tapResponse{Error: tr(lang, "err_checkin_first")})
		return
	}
	var targetBanned int
	var targetName string
	err = tx.QueryRow(`SELECT banned, name FROM users WHERE id=?`, req.To).Scan(&targetBanned, &targetName)
	if err != nil || targetBanned != 0 || targetName == "" {
		writeJSON(w, http.StatusNotFound, tapResponse{Error: tr(lang, "err_not_here")})
		return
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM attendance WHERE user_id=? AND day=?`, req.To, day).Scan(&n); err != nil || n == 0 {
		writeJSON(w, http.StatusNotFound, tapResponse{Error: tr(lang, "err_not_here_today")})
		return
	}
	var days int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM attendance WHERE user_id=?`, u.ID).Scan(&days); err != nil {
		writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
		return
	}
	quota := dailyQuota(days)
	if _, err := tx.Exec(`INSERT OR IGNORE INTO daily_spend(user_id, day, quota_used) VALUES(?,?,0)`, u.ID, day); err != nil {
		writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
		return
	}
	var used int
	if err := tx.QueryRow(`SELECT quota_used FROM daily_spend WHERE user_id=? AND day=?`, u.ID, day).Scan(&used); err != nil {
		writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
		return
	}
	var tickets int64
	if err := tx.QueryRow(`SELECT tickets FROM users WHERE id=?`, u.ID).Scan(&tickets); err != nil {
		writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
		return
	}
	quotaLeft := quota - used
	switch {
	case quotaLeft > 0:
		if _, err := tx.Exec(`UPDATE daily_spend SET quota_used=quota_used+1 WHERE user_id=? AND day=?`, u.ID, day); err != nil {
			writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
			return
		}
		quotaLeft--
	case tickets > 0:
		if _, err := tx.Exec(`UPDATE users SET tickets=tickets-1 WHERE id=?`, u.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
			return
		}
		tickets--
	default:
		writeJSON(w, http.StatusPaymentRequired, tapResponse{Error: tr(lang, "err_no_taps")})
		return
	}
	if _, err := tx.Exec(`INSERT INTO taps(day, from_id, to_id, count) VALUES(?,?,?,1)
		ON CONFLICT(day, from_id, to_id) DO UPDATE SET count=count+1`, day, u.ID, req.To); err != nil {
		writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
		return
	}
	if _, err := tx.Exec(`UPDATE users SET points=points+1 WHERE id=?`, req.To); err != nil {
		writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
		return
	}
	var pairCount, targetPoints int64
	if err := tx.QueryRow(`SELECT count FROM taps WHERE day=? AND from_id=? AND to_id=?`,
		day, u.ID, req.To).Scan(&pairCount); err != nil {
		writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
		return
	}
	if err := tx.QueryRow(`SELECT points FROM users WHERE id=?`, req.To).Scan(&targetPoints); err != nil {
		writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, tapResponse{Error: tr(lang, "err_server")})
		return
	}
	// The recipient's only signal: their points counter ticks up, live.
	a.hub.pushTap(req.To, tapEvent{Type: "tap", From: u.ID, Pair: pairCount, Points: targetPoints})
	writeJSON(w, http.StatusOK, tapResponse{OK: true, QuotaLeft: quotaLeft, Tickets: tickets, MyCount: pairCount})
}

// ---------------------------------------------------------------------------
// Daily share card: only the user's own numbers, never who tapped them.
// ---------------------------------------------------------------------------

type shareData struct {
	Name        string
	Days        int
	TappedToday int64
	Points      int64
	TotalHere   int
	Text        string
	RefLink     string
}

func (a *app) handleShare(w http.ResponseWriter, r *http.Request) {
	u := a.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/auth/google/login", http.StatusFound)
		return
	}
	if u.Name == "" {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	day := dayKey(time.Now())
	days, _ := attendanceDays(a.db, u.ID)
	var tappedToday int64
	_ = a.db.QueryRow(`SELECT COALESCE(SUM(count),0) FROM taps WHERE day=? AND to_id=?`, day, u.ID).Scan(&tappedToday)
	roster, _ := a.roster(day)
	text := trf(langOf(r), "share_text", days, tappedToday, u.Points, len(roster))
	a.render(w, r, "share", shareData{
		Name: u.Name, Days: days, TappedToday: tappedToday, Points: u.Points,
		TotalHere: len(roster), Text: text,
		RefLink: a.cfg.BaseURL + "/?ref=" + u.RefCode,
	})
}

// ---------------------------------------------------------------------------
// Admin (only the env-configured admin Google sub) + misc.
// ---------------------------------------------------------------------------

type adminUser struct {
	ID      int64
	Name    string
	Points  int64
	Tickets int64
	Days    int
	Banned  bool
}

func (a *app) isAdmin(u *user) bool {
	return u != nil && a.cfg.AdminSub != "" && u.Sub == a.cfg.AdminSub
}

func (a *app) handleAdmin(w http.ResponseWriter, r *http.Request) {
	u := a.currentUser(r)
	if !a.isAdmin(u) {
		http.NotFound(w, r)
		return
	}
	rows, err := a.db.Query(`SELECT id, name, points, tickets, banned,
		(SELECT COUNT(*) FROM attendance WHERE user_id=users.id) FROM users ORDER BY id`)
	if err != nil {
		http.Error(w, tr(langOf(r), "err_server"), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var list []adminUser
	for rows.Next() {
		var au adminUser
		var banned int
		if err := rows.Scan(&au.ID, &au.Name, &au.Points, &au.Tickets, &banned, &au.Days); err == nil {
			au.Banned = banned != 0
			list = append(list, au)
		}
	}
	a.render(w, r, "admin", map[string]any{"Users": list})
}

func (a *app) handleAdminBan(w http.ResponseWriter, r *http.Request) {
	u := a.currentUser(r)
	if !a.isAdmin(u) {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	id, err := strconv.ParseInt(r.FormValue("user_id"), 10, 64)
	if err != nil || id == u.ID {
		http.Redirect(w, r, "/admin", http.StatusFound)
		return
	}
	banned := 0
	if r.FormValue("action") == "ban" {
		banned = 1
	}
	_, _ = a.db.Exec(`UPDATE users SET banned=? WHERE id=?`, banned, id)
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.clearSessionCookie(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *app) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "privacy", nil)
}

func (a *app) handleAbout(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "about", nil)
}

const sitemapXML = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://seeyouhere.today/</loc></url>
  <url><loc>https://seeyouhere.today/about</loc></url>
  <url><loc>https://seeyouhere.today/privacy</loc></url>
</urlset>
`

func (a *app) handleSitemap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(sitemapXML))
}

func (a *app) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("Sitemap: https://seeyouhere.today/sitemap.xml\n"))
}

func (a *app) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("ok"))
}
