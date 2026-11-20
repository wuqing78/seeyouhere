package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// One-time self-service credential setup (/setup-creds).
//
// The Google Client Secret lives in the owner's Secure Vault, which no agent
// can read back — so instead of editing the env file by hand, the app itself
// offers a one-time page while (and only while) its OAuth credentials are
// incomplete. At startup, if creds are incomplete, a random token is
// generated; only its SHA-256 hash is kept in memory, and the plaintext is
// written to a "setup-token" file next to the database (mode 600). Whoever
// opens /setup-creds?token=<t> can paste the credentials in; on success the
// in-memory config is updated immediately (no restart needed), the values
// are persisted into the env file, and the token dies.
// ---------------------------------------------------------------------------

// knownClientID prefills the form (the ID is not a secret).
const knownClientID = "507462511521-6q9a0aapkmnibkhav7pfdhtkhgvjtkuq.apps.googleusercontent.com"

type credState struct {
	mu        sync.RWMutex
	id        string
	secret    string
	setupHash []byte // SHA-256 of the one-time setup token; nil when inactive
	tokenPath string
	envFile   string
	idEnv     string // env var names the credentials persist under
	secretEnv string
}

func newCredState(cfg config, envFile string) *credState {
	return &credState{
		id:        cfg.GoogleID,
		secret:    cfg.GoogleSecret,
		tokenPath: filepath.Join(filepath.Dir(cfg.DBPath), "setup-token"),
		envFile:   envFile,
		idEnv:     "GOOGLE_CLIENT_ID",
		secretEnv: "GOOGLE_CLIENT_SECRET",
	}
}

// newGitHubCredState is the GitHub twin of newCredState: same one-time
// setup machinery, its own token file and its own env var names.
func newGitHubCredState(cfg config, envFile string) *credState {
	return &credState{
		id:        cfg.GitHubID,
		secret:    cfg.GitHubSecret,
		tokenPath: filepath.Join(filepath.Dir(cfg.DBPath), "setup-token-github"),
		envFile:   envFile,
		idEnv:     "GITHUB_CLIENT_ID",
		secretEnv: "GITHUB_CLIENT_SECRET",
	}
}

func (cs *credState) configured() bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.id != "" && cs.secret != ""
}

func (cs *credState) get() (id, secret string) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.id, cs.secret
}

// initSetupToken arms the one-time setup page when credentials are
// incomplete, or makes sure no stale token file survives when they are not.
func (cs *credState) initSetupToken() {
	if cs.configured() {
		if err := os.Remove(cs.tokenPath); err != nil && !os.IsNotExist(err) {
			log.Printf("setup-creds: could not remove stale token file: %v", err)
		}
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		log.Printf("setup-creds: could not generate setup token: %v", err)
		return
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	cs.mu.Lock()
	cs.setupHash = sum[:]
	cs.mu.Unlock()
	f, err := os.OpenFile(cs.tokenPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("setup-creds: could not write token file: %v", err)
		return
	}
	if _, err := f.WriteString(token + "\n"); err != nil {
		log.Printf("setup-creds: could not write token file: %v", err)
	}
	_ = f.Close()
	_ = os.Chmod(cs.tokenPath, 0600)
	log.Print("setup-creds: OAuth credentials incomplete — one-time setup page armed (token in setup-token file next to the DB)")
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// tokenValid reports whether the setup page is armed and token matches
// (constant-time comparison of the SHA-256 hashes).
func (cs *credState) tokenValid(token string) bool {
	cs.mu.RLock()
	hash := cs.setupHash
	cs.mu.RUnlock()
	if hash == nil || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare(hashToken(token), hash) == 1
}

// completeSetup installs new credentials in memory and retires the token.
func (cs *credState) completeSetup(id, secret string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.id = id
	cs.secret = secret
	cs.setupHash = nil
}

// rewriteEnvContent returns env-file content with the given client-id and
// client-secret env lines set, preserving every other line as-is.
func rewriteEnvContent(content, id, secret, idKey, secretKey string) string {
	var lines []string
	if content != "" {
		lines = strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	}
	seenID, seenSecret := false, false
	for i, ln := range lines {
		switch {
		case strings.HasPrefix(ln, idKey+"="):
			lines[i] = idKey + "=" + id
			seenID = true
		case strings.HasPrefix(ln, secretKey+"="):
			lines[i] = secretKey + "=" + secret
			seenSecret = true
		}
	}
	if !seenID {
		lines = append(lines, idKey+"="+id)
	}
	if !seenSecret {
		lines = append(lines, secretKey+"="+secret)
	}
	return strings.Join(lines, "\n") + "\n"
}

// persistEnvCreds rewrites the env file with the new credentials, keeping
// mode 600 (written via a temp file + rename so a crash can't truncate it).
func persistEnvCreds(path, id, secret, idKey, secretKey string) error {
	content := ""
	if data, err := os.ReadFile(path); err == nil {
		content = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(rewriteEnvContent(content, id, secret, idKey, secretKey)), 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0600)
}

// persist saves this state's credentials into its env file.
func (cs *credState) persist(id, secret string) error {
	return persistEnvCreds(cs.envFile, id, secret, cs.idEnv, cs.secretEnv)
}

type setupCredsData struct {
	Token    string
	ClientID string
	Error    string
}

func (a *app) handleSetupCreds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.handleSetupCredsGet(w, r)
	case http.MethodPost:
		a.handleSetupCredsPost(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (a *app) handleSetupCredsGet(w http.ResponseWriter, r *http.Request) {
	if a.creds.configured() || !a.creds.tokenValid(r.URL.Query().Get("token")) {
		http.NotFound(w, r)
		return
	}
	id, _ := a.creds.get()
	if id == "" {
		id = knownClientID
	}
	a.render(w, r, "setupcreds", setupCredsData{Token: r.URL.Query().Get("token"), ClientID: id})
}

func (a *app) handleSetupCredsPost(w http.ResponseWriter, r *http.Request) {
	if !a.limiter.allow(clientIP(r), 10, time.Hour) {
		http.Error(w, "Too many attempts — please wait a bit.", http.StatusTooManyRequests)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.NotFound(w, r)
		return
	}
	token := r.FormValue("token")
	if a.creds.configured() || !a.creds.tokenValid(token) {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSpace(r.FormValue("client_id"))
	secret := strings.TrimSpace(r.FormValue("client_secret"))
	fail := func(msg string) {
		a.render(w, r, "setupcreds", setupCredsData{Token: token, ClientID: id, Error: msg})
	}
	if !strings.HasSuffix(id, ".apps.googleusercontent.com") {
		fail("That Client ID doesn't look right — it should end in .apps.googleusercontent.com.")
		return
	}
	if len(secret) < 10 {
		fail("That Client Secret looks too short — please paste the whole thing.")
		return
	}
	if err := a.creds.persist(id, secret); err != nil {
		log.Printf("setup-creds: could not persist env file: %v", err)
		fail("The server couldn't save the credentials — please try again.")
		return
	}
	a.creds.completeSetup(id, secret)
	if err := os.Remove(a.creds.tokenPath); err != nil && !os.IsNotExist(err) {
		log.Printf("setup-creds: could not delete token file: %v", err)
	}
	log.Print("setup-creds: Google OAuth credentials configured via the one-time setup page; sign-in is live")
	a.render(w, r, "setupcredsdone", nil)
}

// ---------------------------------------------------------------------------
// One-time credential setup for GitHub sign-in (/setup-github): the same
// machinery as /setup-creds, armed only while the GitHub credentials are
// incomplete, with its own token file (setup-token-github).
// ---------------------------------------------------------------------------

type setupGitHubData struct {
	Token    string
	ClientID string
	Error    string
}

func (a *app) handleSetupGitHub(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if a.ghCreds.configured() || !a.ghCreds.tokenValid(r.URL.Query().Get("token")) {
			http.NotFound(w, r)
			return
		}
		id, _ := a.ghCreds.get()
		a.render(w, r, "setupgithub", setupGitHubData{Token: r.URL.Query().Get("token"), ClientID: id})
	case http.MethodPost:
		if !a.limiter.allow(clientIP(r), 10, time.Hour) {
			http.Error(w, "Too many attempts — please wait a bit.", http.StatusTooManyRequests)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.NotFound(w, r)
			return
		}
		token := r.FormValue("token")
		if a.ghCreds.configured() || !a.ghCreds.tokenValid(token) {
			http.NotFound(w, r)
			return
		}
		id := strings.TrimSpace(r.FormValue("client_id"))
		secret := strings.TrimSpace(r.FormValue("client_secret"))
		fail := func(msg string) {
			a.render(w, r, "setupgithub", setupGitHubData{Token: token, ClientID: id, Error: msg})
		}
		if len(id) < 10 || strings.ContainsAny(id, " \t") {
			fail("That Client ID doesn't look right — please paste the whole thing.")
			return
		}
		if len(secret) < 20 {
			fail("That Client Secret looks too short — please paste the whole thing.")
			return
		}
		if err := a.ghCreds.persist(id, secret); err != nil {
			log.Printf("setup-github: could not persist env file: %v", err)
			fail("The server couldn't save the credentials — please try again.")
			return
		}
		a.ghCreds.completeSetup(id, secret)
		if err := os.Remove(a.ghCreds.tokenPath); err != nil && !os.IsNotExist(err) {
			log.Printf("setup-github: could not delete token file: %v", err)
		}
		log.Print("setup-github: GitHub OAuth credentials configured via the one-time setup page; GitHub sign-in is live")
		a.render(w, r, "setupgithubdone", nil)
	default:
		http.NotFound(w, r)
	}
}
