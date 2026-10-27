package credentials

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourceValidationAndRotation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "auth.json")
	now := time.Unix(2000, 0)
	write := func(account string, expiry int64, suffix string) {
		claims, _ := json.Marshal(map[string]any{"exp": expiry, "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": account}})
		token := "header." + base64.RawURLEncoding.EncodeToString(claims) + "." + suffix
		b, _ := json.Marshal(map[string]any{"tokens": map[string]any{"access_token": token, "account_id": account, "refresh_token": "never-export-this"}})
		if err := os.WriteFile(p, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("account-a", 3000, "first")
	s := Source{Path: p, AccountID: "account-a"}
	first, err := s.Read(now)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(first.Params())
	if strings.Contains(string(b), "never-export") {
		t.Fatal("refresh credential exported")
	}
	write("account-a", 4000, "second")
	second, err := s.Read(now)
	if err != nil || first.AccessToken == second.AccessToken {
		t.Fatal("rotation not loaded", err)
	}
	write("account-b", 4000, "other")
	if _, err = s.Read(now); err == nil {
		t.Fatal("account switch allowed")
	}
	write("account-a", 2001, "expired")
	if _, err = s.Read(now); err == nil {
		t.Fatal("near-expiry token accepted")
	}
	os.WriteFile(p, []byte(`{"tokens":{"access_token":"private-malformed-value"}}`), 0600)
	if _, err = s.Read(now); err == nil || strings.Contains(err.Error(), "private-malformed") {
		t.Fatal("invalid token accepted or leaked", err)
	}
	os.Remove(p)
	if _, err = s.Read(now); err == nil || strings.Contains(err.Error(), p) {
		t.Fatal("missing file accepted or path leaked", err)
	}
}

func TestSourceRequiresAccountPin(t *testing.T) {
	if _, err := (Source{}).Read(time.Now()); err == nil {
		t.Fatal("missing configuration accepted")
	}
}
