package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupTokenHash(t *testing.T) {
	cs := &credState{}
	if cs.tokenValid("anything") {
		t.Fatal("tokenValid must be false while no token is armed")
	}
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	h := hashToken(token)
	cs.setupHash = h
	if !cs.tokenValid(token) {
		t.Fatal("correct token should validate")
	}
	if cs.tokenValid(token + "x") {
		t.Fatal("extended token must not validate")
	}
	if cs.tokenValid("") {
		t.Fatal("empty token must not validate")
	}
	if cs.tokenValid(strings.Repeat("0", 64)) {
		t.Fatal("wrong token of the right shape must not validate")
	}
	// Hash is stable and 32 bytes.
	if len(h) != 32 {
		t.Fatalf("sha256 hash length = %d, want 32", len(h))
	}
	if got := hashToken(token); string(got) != string(h) {
		t.Fatal("hashToken must be deterministic")
	}
	// completeSetup retires the token.
	cs.completeSetup("id.apps.googleusercontent.com", "secretsecret")
	if cs.tokenValid(token) {
		t.Fatal("token must be dead after completeSetup")
	}
	if !cs.configured() {
		t.Fatal("creds should be configured after completeSetup")
	}
}

func TestRewriteEnvContent(t *testing.T) {
	id := "123-x.apps.googleusercontent.com"
	secret := "GOCSPX-dummysecret"

	// Replaces existing lines, preserves everything else.
	in := "# comment\nSYH_ADDR=127.0.0.1:18081\nGOOGLE_CLIENT_ID=\nGOOGLE_CLIENT_SECRET=\nSYH_ADMIN_SUB=\n"
	out := rewriteEnvContent(in, id, secret)
	for _, want := range []string{
		"# comment", "SYH_ADDR=127.0.0.1:18081", "SYH_ADMIN_SUB=",
		"GOOGLE_CLIENT_ID=" + id, "GOOGLE_CLIENT_SECRET=" + secret,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rewritten env missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "GOOGLE_CLIENT_ID=") != 1 || strings.Count(out, "GOOGLE_CLIENT_SECRET=") != 1 {
		t.Fatalf("keys must appear exactly once:\n%s", out)
	}

	// Appends when the keys are absent.
	out = rewriteEnvContent("SYH_ADDR=x\n", id, secret)
	if !strings.Contains(out, "GOOGLE_CLIENT_ID="+id+"\n") || !strings.Contains(out, "GOOGLE_CLIENT_SECRET="+secret+"\n") {
		t.Fatalf("missing keys should be appended:\n%s", out)
	}

	// Works from empty content.
	out = rewriteEnvContent("", id, secret)
	if out != "GOOGLE_CLIENT_ID="+id+"\nGOOGLE_CLIENT_SECRET="+secret+"\n" {
		t.Fatalf("empty content rewrite wrong: %q", out)
	}
}

func TestPersistEnvCreds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seeyouhere.env")
	if err := os.WriteFile(path, []byte("SYH_ADDR=127.0.0.1:18081\nGOOGLE_CLIENT_ID=\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := persistEnvCreds(path, "123-x.apps.googleusercontent.com", "GOCSPX-dummysecret"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "GOOGLE_CLIENT_ID=123-x.apps.googleusercontent.com") ||
		!strings.Contains(s, "GOOGLE_CLIENT_SECRET=GOCSPX-dummysecret") ||
		!strings.Contains(s, "SYH_ADDR=127.0.0.1:18081") {
		t.Fatalf("persisted env wrong:\n%s", s)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("env file mode = %o, want 600", fi.Mode().Perm())
	}
}
