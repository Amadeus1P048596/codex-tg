// Package credentials reads access tokens from an explicitly configured owner.
// It never rotates refresh tokens or writes the owner's credential store.
package credentials

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

type Source struct{ Path, AccountID string }
type Token struct{ AccessToken, AccountID string }

func (t Token) Params() map[string]any {
	return map[string]any{"accessToken": t.AccessToken, "chatgptAccountId": t.AccountID}
}

func (s Source) Read(now time.Time) (Token, error) {
	fail := func(message string) (Token, error) { return Token{}, errors.New("credential watchdog: " + message) }
	if s.Path == "" || s.AccountID == "" {
		return fail("configure an auth source and pinned account")
	}
	info, err := os.Stat(s.Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return fail("source unavailable or not a bounded regular file; sign in to the credential owner")
	}
	f, err := os.Open(s.Path)
	if err != nil {
		return fail("source unavailable; sign in to the credential owner")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return fail("source must be a bounded regular file")
	}
	var auth struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if json.NewDecoder(io.LimitReader(f, 1024*1024)).Decode(&auth) != nil {
		return fail("source JSON is invalid")
	}
	parts := strings.Split(auth.Tokens.AccessToken, ".")
	if len(parts) != 3 {
		return fail("source access token is invalid")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fail("source access token is invalid")
	}
	var claims struct {
		Exp  int64 `json:"exp"`
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(data, &claims) != nil {
		return fail("source access token is invalid")
	}
	if auth.Tokens.AccountID != s.AccountID || claims.Auth.AccountID != s.AccountID {
		return fail("account changed; explicit reconfiguration required")
	}
	if claims.Exp <= now.Add(30*time.Second).Unix() {
		return fail("source token expired; sign in or refresh in the credential owner")
	}
	// JWT decoding is local expiry/account screening, not signature validation.
	// OpenAI remains the authority for actual authorization.
	return Token{AccessToken: auth.Tokens.AccessToken, AccountID: s.AccountID}, nil
}
