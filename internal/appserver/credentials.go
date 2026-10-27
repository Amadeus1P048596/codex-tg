package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"
)

const credentialsLoaded = "access token loaded (server authorization checked on use)"

// CredentialStatus exposes no token, account identifier or source path.
func (c *Client) CredentialStatus() string {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if c.authSource.Path == "" {
		return "disabled"
	}
	if c.authStatus == "" {
		return "waiting for source"
	}
	return c.authStatus
}

func (c *Client) syncCredentials(ctx context.Context, generation uint64) error {
	if c.authSource.Path == "" {
		return nil
	}
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if !c.isStartedGeneration(generation) {
		return errors.New("credential watchdog: stale session")
	}
	token, err := c.authSource.Read(time.Now())
	if err != nil {
		c.authStatus = err.Error()
		return err
	}
	if token.AccessToken == c.authRejected && c.authGeneration == generation {
		c.authStatus = "credential watchdog: source token rejected; sign in to the credential owner"
		return errors.New(c.authStatus)
	}
	if token.AccessToken == c.authToken && c.authGeneration == generation {
		c.authStatus = credentialsLoaded
		return nil
	}
	params := token.Params()
	params["type"] = "chatgptAuthTokens"
	// Never return raw RPC errors: third-party runtimes could echo credentials.
	if _, err = c.requestForGeneration(ctx, "account/login/start", params, generation); err != nil {
		c.authStatus = "credential watchdog: external-token login failed; check CLI compatibility and source login"
		return errors.New(c.authStatus)
	}
	if !c.isStartedGeneration(generation) {
		return errors.New("credential watchdog: stale session")
	}
	c.authToken = token.AccessToken
	c.authRejected = ""
	c.authGeneration = generation
	c.authStatus = credentialsLoaded
	return nil
}

func (c *Client) credentialWatchdog(ctx context.Context, generation uint64) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !c.isStartedGeneration(generation) {
				return
			}
			_ = c.syncCredentials(ctx, generation)
		}
	}
}

// Handle this protocol request internally, never as a Telegram approval. The
// response is bound to the exact process generation that requested it.
func (c *Client) refreshCredentials(id any, params map[string]any, generation uint64) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if !c.isStartedGeneration(generation) {
		return
	}
	token, err := c.authSource.Read(time.Now())
	if previous, ok := params["previousAccountId"].(string); ok && previous != "" && previous != c.authSource.AccountID {
		err = errors.New("credential watchdog: account changed; explicit reconfiguration required")
	}
	if err == nil && (token.AccessToken == c.authToken || token.AccessToken == c.authRejected) {
		err = errors.New("credential watchdog: source token rejected; sign in to the credential owner")
	}
	response := map[string]any{"jsonrpc": "2.0", "id": id}
	if err != nil {
		c.authRejected = c.authToken
		c.authStatus = err.Error()
		response["error"] = map[string]any{"code": -32001, "message": err.Error()}
	} else {
		response["result"] = token.Params()
	}
	data, _ := json.Marshal(response)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.started || c.generation != generation || c.stdin == nil {
		return
	}
	if _, writeErr := io.WriteString(c.stdin, string(data)+"\n"); writeErr == nil && err == nil {
		c.authToken = token.AccessToken
		c.authRejected = ""
		c.authGeneration = generation
		c.authStatus = credentialsLoaded
	}
}

// External credentials do not grant permission to share Desktop runtime state.
func (c *Client) validateCredentialIsolation() error {
	if c.codexHome == "" || c.authSource.AccountID == "" || !filepath.IsAbs(c.authSource.Path) || !filepath.IsAbs(c.codexHome) {
		return errors.New("credential watchdog: configure an absolute source, pinned account and separate absolute CODEX_HOME")
	}
	sourceHome := filepath.Dir(c.authSource.Path)
	privateHome := c.codexHome
	if resolved, err := filepath.EvalSymlinks(sourceHome); err == nil {
		sourceHome = resolved
	}
	if resolved, err := filepath.EvalSymlinks(privateHome); err == nil {
		privateHome = resolved
	}
	if filepath.Clean(sourceHome) == filepath.Clean(privateHome) {
		return errors.New("credential watchdog: source and Telegram runtime homes must be separate")
	}
	return nil
}

func (c *Client) childEnvironment() []string {
	env := appServerEnvironment(c.codexHome)
	if c.authSource.Path == "" {
		return env
	}
	out := make([]string, 0, len(env))
	for _, pair := range env {
		key, _, _ := strings.Cut(pair, "=")
		if strings.EqualFold(key, "OPENAI_API_KEY") || strings.EqualFold(key, "CODEX_API_KEY") {
			continue
		}
		out = append(out, pair)
	}
	return out
}
