package appserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func credentialFixture(t *testing.T) (*Client, func(string), *io.PipeReader) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "auth.json")
	write := func(suffix string) {
		claims, _ := json.Marshal(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "test-account"}})
		data, _ := json.Marshal(map[string]any{"tokens": map[string]any{"access_token": "header." + base64.RawURLEncoding.EncodeToString(claims) + "." + suffix, "account_id": "test-account", "refresh_token": "never-copy"}})
		if err := os.WriteFile(p, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("one")
	c := NewClient("codex", "stdio://", t.TempDir(), time.Second, ClientOptions{AuthSourceFile: p, AuthAccountID: "test-account", CodexHome: t.TempDir()})
	r, w := io.Pipe()
	c.started = true
	c.generation = 1
	c.stdin = w
	t.Cleanup(func() { r.Close(); w.Close() })
	return c, write, r
}

func TestCredentialWatchdogRotationAndRefresh(t *testing.T) {
	c, write, r := credentialFixture(t)
	messages := make(chan map[string]any, 10)
	go func() {
		d := json.NewDecoder(r)
		for {
			var m map[string]any
			if d.Decode(&m) != nil {
				return
			}
			messages <- m
			if m["method"] == "account/login/start" {
				c.handlePayload(map[string]any{"id": m["id"], "result": map[string]any{"type": "chatgptAuthTokens"}}, 1)
			}
		}
	}()
	if err := c.syncCredentials(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	first := <-messages
	if first["params"].(map[string]any)["type"] != "chatgptAuthTokens" {
		t.Fatal(first)
	}
	if err := c.syncCredentials(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-messages:
		t.Fatal("unchanged token logged in again")
	default:
	}
	c.handlePayload(map[string]any{"id": float64(42), "method": "account/chatgptAuthTokens/refresh", "params": map[string]any{"previousAccountId": "test-account"}}, 1)
	select {
	case m := <-messages:
		if m["error"] == nil {
			t.Fatal("rejected token reused")
		}
	case <-time.After(time.Second):
		t.Fatal("refresh request not answered")
	}
	write("two")
	if err := c.syncCredentials(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	<-messages
	if c.CredentialStatus() != "access token loaded (server authorization checked on use)" {
		t.Fatal(c.CredentialStatus())
	}
	write("three")
	c.handlePayload(map[string]any{"id": "fresh", "method": "account/chatgptAuthTokens/refresh", "params": map[string]any{"previousAccountId": "test-account"}}, 1)
	select {
	case m := <-messages:
		b, _ := json.Marshal(m)
		if m["result"] == nil || strings.Contains(string(b), "never-copy") {
			t.Fatal("bad refresh response")
		}
	case <-time.After(time.Second):
		t.Fatal("refresh timed out")
	}
	if err := c.syncCredentials(context.Background(), 0); err == nil {
		t.Fatal("stale generation accepted")
	}
}

func TestCredentialWatchdogFailsClosedAndDoesNotLeak(t *testing.T) {
	c, _, _ := credentialFixture(t)
	os.Remove(c.authSource.Path)
	if _, err := c.TurnStart(context.Background(), "t", "hello", "", TurnStartOptions{}); err == nil || strings.Contains(err.Error(), c.authSource.Path) {
		t.Fatal("turn did not fail safely", err)
	}
	cmd, err := c.buildCommand()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), `cli_auth_credentials_store="ephemeral"`) {
		t.Fatal("old stored refresh tokens remain reachable")
	}
}

func TestCredentialWatchdogRejectsWrongAccountAndStaleRefresh(t *testing.T) {
	c, _, r := credentialFixture(t)
	done := make(chan map[string]any, 1)
	go func() { var m map[string]any; json.NewDecoder(r).Decode(&m); done <- m }()
	c.handlePayload(map[string]any{"id": 1, "method": "account/chatgptAuthTokens/refresh", "params": map[string]any{"previousAccountId": "another-account"}}, 1)
	select {
	case m := <-done:
		if m["error"] == nil {
			t.Fatal("cross-account refresh accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("no error response")
	}
	c.refreshCredentials("stale", nil, 0) // must return without writing to the new process
}

func TestCredentialWatchdogSourceRecovery(t *testing.T) {
	c, write, r := credentialFixture(t)
	go func() {
		d := json.NewDecoder(r)
		for {
			var m map[string]any
			if d.Decode(&m) != nil {
				return
			}
			c.handlePayload(map[string]any{"id": m["id"], "result": map[string]any{}}, 1)
		}
	}()
	os.Remove(c.authSource.Path)
	if err := c.syncCredentials(context.Background(), 1); err == nil {
		t.Fatal("missing source accepted")
	}
	write("recovered")
	if err := c.syncCredentials(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if c.CredentialStatus() != credentialsLoaded {
		t.Fatal(c.CredentialStatus())
	}
}

func TestCredentialWatchdogRequiresIsolatedHome(t *testing.T) {
	c, _, _ := credentialFixture(t)
	c.codexHome = filepath.Dir(c.authSource.Path)
	if _, err := c.buildCommand(); err == nil {
		t.Fatal("shared Desktop home accepted")
	}
	c.codexHome = t.TempDir()
	t.Setenv("OPENAI_API_KEY", "do-not-inherit")
	cmd, err := c.buildCommand()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(cmd.Env, "\n"), "do-not-inherit") {
		t.Fatal("API key fallback inherited")
	}
}

// Opt-in protocol smoke: no user turns, service restart, or Telegram delivery.
func TestCredentialWatchdogLive(t *testing.T) {
	if os.Getenv("CODEX_TG_AUTH_LIVE_TEST") != "1" {
		t.Skip("explicit live opt-in required")
	}
	source := os.Getenv("CODEX_TG_AUTH_TEST_SOURCE")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal("source unavailable")
	}
	var auth struct {
		Tokens struct {
			AccountID string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.Unmarshal(data, &auth) != nil {
		t.Fatal("source invalid")
	}
	home := t.TempDir()
	c := NewClient(os.Getenv("CODEX_TG_AUTH_TEST_BIN"), "stdio://", home, 15*time.Second, ClientOptions{CodexHome: home, AuthSourceFile: source, AuthAccountID: auth.Tokens.AccountID})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	defer c.Close()
	if err := c.Start(ctx); err != nil {
		t.Fatal("App Server startup failed")
	}
	if c.CredentialStatus() != credentialsLoaded {
		t.Fatal(c.CredentialStatus())
	}
	if _, err := c.Request(ctx, "account/rateLimits/read", map[string]any{}); err != nil {
		t.Fatal("account authorization probe failed")
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("credentials unexpectedly persisted")
	}
}

func TestCredentialWatchdogLoginErrorsAreSanitized(t *testing.T) {
	c, _, r := credentialFixture(t)
	go func() {
		var m map[string]any
		json.NewDecoder(r).Decode(&m)
		c.handlePayload(map[string]any{"id": m["id"], "error": map[string]any{"message": "private-error-token"}}, 1)
	}()
	err := c.syncCredentials(context.Background(), 1)
	if err == nil || strings.Contains(err.Error(), "private-error-token") || strings.Contains(c.CredentialStatus(), "private-error-token") {
		t.Fatal("unsafe login error")
	}
}
