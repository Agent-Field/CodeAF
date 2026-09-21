package codexauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/trace"
)

const (
	DefaultIssuer  = "https://auth.openai.com"
	DefaultBackend = "https://chatgpt.com/backend-api/codex"
	ClientID       = "app_EMoamEEZ73f0CkXaXp7hrann"
	Sentinel       = "chatgpt"
	Originator     = "codex_cli_rs"
	clientVersion  = "0.144.1"
)

// signInExpiredError is terminal before the provider's ordinary transport
// recovery. The issuer has already made the one decision another send cannot
// improve, and the session owns the actionable sentence this value carries.
type signInExpiredError struct{}

func (*signInExpiredError) Error() string {
	return "codex sign-in has expired · /connect or codeaf connect codex signs in again"
}

// TerminalTransportFailure tells the shared provider dispatcher not to spend
// its retry window asking the same expired sign-in again.
func (*signInExpiredError) TerminalTransportFailure() bool { return true }

// ErrSignInExpired is the one actionable sentence returned when the issuer no
// longer accepts a profile's rotating refresh token.
var ErrSignInExpired error = &signInExpiredError{}

// Tokens is the complete durable answer from one browser sign-in.
type Tokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	AccountID    string    `json:"account_id"`
	Email        string    `json:"email"`
	Plan         string    `json:"plan"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastRefresh  time.Time `json:"last_refresh"`
}

// Issuer returns the configured issuer without changing the fixed callback.
func Issuer() string {
	if value := strings.TrimRight(strings.TrimSpace(env.Value("CODEAF_CODEX_ISSUER")), "/"); value != "" {
		return value
	}
	return DefaultIssuer
}

// Backend returns the configured ChatGPT backend used by both listing and turns.
func Backend() string {
	if value := strings.TrimRight(strings.TrimSpace(env.Value("CODEAF_CODEX_BACKEND")), "/"); value != "" {
		return value
	}
	return DefaultBackend
}

// Path names the owner-readable token file in a profile.
func Path(profileDir string) string { return profilePath(profileDir, "codex.json") }

// ModelsPath names the non-secret listing cached beside the tokens.
func ModelsPath(profileDir string) string { return profilePath(profileDir, "codex-models.json") }

func profilePath(profileDir, name string) string {
	if profileDir = strings.TrimSpace(profileDir); profileDir != "" {
		return filepath.Join(profileDir, name)
	}
	return home.Join(name)
}

// Load reads the current token set and immediately registers every credential
// with the record scrubber.
func Load(profileDir string) (Tokens, error) {
	var tokens Tokens
	raw, err := os.ReadFile(Path(profileDir))
	if err != nil {
		return tokens, err
	}
	if err := json.Unmarshal(raw, &tokens); err != nil {
		return Tokens{}, fmt.Errorf("read codex sign-in: %w", err)
	}
	register(tokens)
	return tokens, nil
}

// Save replaces the token file with an owner-only file.
func Save(profileDir string, tokens Tokens) error {
	register(tokens)
	raw, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("write codex sign-in: %w", err)
	}
	path := Path(profileDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("write codex sign-in: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".codex-*.json")
	if err != nil {
		return fmt.Errorf("write codex sign-in: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(raw)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporaryPath, path)
	}
	if err != nil {
		return fmt.Errorf("write codex sign-in: %w", err)
	}
	return os.Chmod(path, 0o600)
}

// Remove forgets the browser sign-in and its model listing.
func Remove(profileDir string) error {
	for _, path := range []string{Path(profileDir), ModelsPath(profileDir)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Connected reports whether a usable sign-in currently exists.
func Connected(profileDir string) bool {
	tokens, err := Load(profileDir)
	return err == nil && strings.TrimSpace(tokens.AccessToken) != "" && strings.TrimSpace(tokens.RefreshToken) != ""
}

func register(tokens Tokens) {
	trace.Secret(tokens.AccessToken)
	trace.Secret(tokens.RefreshToken)
	trace.Secret(tokens.IDToken)
}

type tokenClaims struct {
	Email string `json:"email"`
	Exp   int64  `json:"exp"`
	Auth  struct {
		AccountID string `json:"chatgpt_account_id"`
		Plan      string `json:"chatgpt_plan_type"`
	} `json:"https://api.openai.com/auth"`
}

func claimsFrom(token string) (tokenClaims, error) {
	var claims tokenClaims
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return claims, errors.New("token has no claims")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, err
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return claims, err
	}
	return claims, nil
}
