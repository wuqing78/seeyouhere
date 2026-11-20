package main

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed templates
var templatesFS embed.FS

//go:embed assets
var assetsFS embed.FS

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func comma(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func main() {
	cfg := config{
		Addr:          envOr("SYH_ADDR", "127.0.0.1:18081"),
		DBPath:        envOr("SYH_DB", "./seeyouhere.db"),
		BaseURL:       strings.TrimSuffix(envOr("SYH_BASE_URL", "https://seeyouhere.today"), "/"),
		GoogleID:      os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleSecret:  os.Getenv("GOOGLE_CLIENT_SECRET"),
		GitHubID:      os.Getenv("GITHUB_CLIENT_ID"),
		GitHubSecret:  os.Getenv("GITHUB_CLIENT_SECRET"),
		SessionSecret: os.Getenv("SYH_SESSION_SECRET"),
		AdminSub:      os.Getenv("SYH_ADMIN_SUB"),
	}
	if cfg.SessionSecret == "" {
		cfg.SessionSecret = randomHex(32)
		log.Print("WARNING: SYH_SESSION_SECRET not set — using a random one; sessions will not survive a restart")
	}
	if !cfg.oauthConfigured() {
		log.Print("WARNING: GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET not set — sign-in is disabled")
	}

	db, err := openDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	tmpls := map[string]*template.Template{}
	for _, lang := range []string{"en", "zh"} {
		lang := lang
		t, err := template.New("").Funcs(template.FuncMap{
			"comma":   comma,
			"curlang": func() string { return lang },
			"tr":      func(key string) string { return tr(lang, key) },
			"trf":     func(key string, args ...any) string { return trf(lang, key, args...) },
		}).ParseFS(templatesFS, "templates/*.html")
		if err != nil {
			log.Fatalf("templates (%s): %v", lang, err)
		}
		tmpls[lang] = t
	}

	envFile := envOr("SYH_ENV_FILE", filepath.Join(filepath.Dir(cfg.DBPath), "seeyouhere.env"))
	creds := newCredState(cfg, envFile)
	ghCreds := newGitHubCredState(cfg, envFile)
	a := &app{db: db, tmpls: tmpls, cfg: cfg, creds: creds, ghCreds: ghCreds, hub: newHub(), limiter: newRateLimiter()}
	creds.initSetupToken()
	ghCreds.initSetupToken()

	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleHome)
	mux.HandleFunc("/auth/google/login", a.handleGoogleLogin)
	mux.HandleFunc("/auth/google/callback", a.handleGoogleCallback)
	mux.HandleFunc("/auth/github/login", a.handleGitHubLogin)
	mux.HandleFunc("/auth/github/callback", a.handleGitHubCallback)
	mux.HandleFunc("/guest", a.handleGuestEnter)
	mux.HandleFunc("/setup-creds", a.handleSetupCreds)
	mux.HandleFunc("/setup-github", a.handleSetupGitHub)
	mux.HandleFunc("/setup", a.handleSetup)
	mux.HandleFunc("/setup/confirm", a.handleSetupConfirm)
	mux.HandleFunc("/checkin", a.handleCheckin)
	mux.HandleFunc("/tap", a.handleTap)
	mux.HandleFunc("/ws", a.handleWS)
	mux.HandleFunc("/share", a.handleShare)
	mux.HandleFunc("/logout", a.handleLogout)
	mux.HandleFunc("/admin", a.handleAdmin)
	mux.HandleFunc("/admin/ban", a.handleAdminBan)
	mux.HandleFunc("/privacy", a.handlePrivacy)
	mux.HandleFunc("/about", a.handleAbout)
	mux.HandleFunc("/sitemap.xml", a.handleSitemap)
	mux.HandleFunc("/robots.txt", a.handleRobots)
	mux.HandleFunc("/lang", a.handleLang)
	mux.HandleFunc("/healthz", a.handleHealthz)
	staticSub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		log.Fatalf("assets: %v", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
	mux.HandleFunc("/favicon.svg", func(w http.ResponseWriter, r *http.Request) {
		data, _ := assetsFS.ReadFile("assets/dots.svg")
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write(data)
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("seeyouhere listening on %s (base %s)", cfg.Addr, cfg.BaseURL)
	log.Fatal(srv.ListenAndServe())
}
