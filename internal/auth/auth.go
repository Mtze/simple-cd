// Package auth implements the Keycloak login: OIDC authorization code flow
// with PKCE, server-side sessions in Postgres and RP-initiated logout.
// Roles come from the client roles in the ID token
// (resource_access.<client>.roles).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Mtze/simple-cd/internal/core"
	"github.com/Mtze/simple-cd/internal/store"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	BaseURL      string // public URL of simple-cd, e.g. https://cd.example.org
	SessionTTL   time.Duration
}

type Auth struct {
	cfg       Config
	store     *store.Store
	provider  *oidc.Provider
	verifier  *oidc.IDTokenVerifier
	oauth     oauth2.Config
	endURL    string
	secure    bool
	cookieKey string
}

// New discovers the provider. It fails fast when Keycloak is unreachable,
// so a misconfigured deployment does not start half-working.
func New(ctx context.Context, cfg Config, s *store.Store) (*Auth, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&meta); err != nil {
		return nil, err
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 4 * time.Hour
	}
	secure := strings.HasPrefix(cfg.BaseURL, "https://")
	name := "simplecd_session"
	if secure {
		name = "__Host-simplecd_session" // only sent over https, host-only, path /
	}
	return &Auth{
		cfg: cfg, store: s, provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: provider.Endpoint(),
			RedirectURL: strings.TrimSuffix(cfg.BaseURL, "/") + "/auth/callback",
			Scopes:      []string{oidc.ScopeOpenID, "profile", "email"},
		},
		endURL: meta.EndSession, secure: secure, cookieKey: name,
	}, nil
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// safeReturn only allows local paths, so the login cannot redirect elsewhere.
func safeReturn(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") || strings.HasPrefix(p, "/auth/") {
		return "/"
	}
	return p
}

// Login starts the authorization code flow with PKCE.
func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	state, nonce, verifier := randomString(24), randomString(24), oauth2.GenerateVerifier()
	err := a.store.CreateAuthRequest(r.Context(), store.AuthRequest{
		State: state, Verifier: verifier, Nonce: nonce,
		ReturnTo: safeReturn(r.URL.Query().Get("return")), ExpiresAt: time.Now().Add(10 * time.Minute),
	})
	if err != nil {
		slog.Error("create auth request", "err", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	url := a.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce))
	http.Redirect(w, r, url, http.StatusFound)
}

// Callback finishes the login and creates the session.
func (a *Auth) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		http.Error(w, "login failed: "+e, http.StatusUnauthorized)
		return
	}
	req, err := a.store.TakeAuthRequest(ctx, q.Get("state"))
	if err != nil {
		http.Error(w, "login expired, please try again", http.StatusBadRequest)
		return
	}
	tok, err := a.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(req.Verifier))
	if err != nil {
		slog.Warn("code exchange failed", "err", err)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idTok, err := a.verifier.Verify(ctx, rawID)
	if err != nil || idTok.Nonce != req.Nonce {
		slog.Warn("id token rejected", "err", err)
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	var claims Claims
	if err := idTok.Claims(&claims); err != nil {
		http.Error(w, "login failed", http.StatusUnauthorized)
		return
	}
	user := claims.User(a.cfg.ClientID)
	id := randomString(32)
	err = a.store.CreateSession(ctx, store.Session{
		IDHash: hashID(id), Subject: idTok.Subject, Username: user.Username, Name: user.Name, Email: user.Email,
		Roles: user.Roles, IDToken: rawID, ExpiresAt: time.Now().Add(a.cfg.SessionTTL),
	})
	if err != nil {
		slog.Error("create session", "err", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	a.store.Audit(ctx, user.Username, "auth.login", "", map[string]any{"roles": user.Roles})
	http.SetCookie(w, a.cookie(id, int(a.cfg.SessionTTL.Seconds())))
	http.Redirect(w, r, req.ReturnTo, http.StatusFound)
}

func (a *Auth) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: a.cookieKey, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	}
}

// Logout deletes the session and returns the Keycloak end-session URL.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	var idToken string
	if c, err := r.Cookie(a.cookieKey); err == nil {
		if s, err := a.store.Session(r.Context(), hashID(c.Value)); err == nil {
			idToken = s.IDToken
			a.store.Audit(r.Context(), s.Username, "auth.logout", "", nil)
		}
		a.store.DeleteSession(r.Context(), hashID(c.Value))
	}
	http.SetCookie(w, a.cookie("", -1))
	logout := strings.TrimSuffix(a.cfg.BaseURL, "/") + "/"
	if a.endURL != "" {
		q := url.Values{"client_id": {a.cfg.ClientID}, "post_logout_redirect_uri": {logout}}
		if idToken != "" {
			q.Set("id_token_hint", idToken)
		}
		logout = a.endURL + "?" + q.Encode()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"logoutUrl": logout})
}

type ctxKey struct{}

// Middleware loads the session. Requests without one get 401; the UI then
// sends the browser to /auth/login, which is instant with an SSO session.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(a.cookieKey)
		if err != nil {
			unauthenticated(w)
			return
		}
		s, err := a.store.Session(r.Context(), hashID(c.Value))
		if errors.Is(err, store.ErrNotFound) {
			unauthenticated(w)
			return
		}
		if err != nil {
			http.Error(w, "session lookup failed", http.StatusInternalServerError)
			return
		}
		u := core.User{Username: s.Username, Name: s.Name, Email: s.Email, Roles: s.Roles}
		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), u)))
	})
}

func unauthenticated(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"unauthenticated","code":"unauthenticated"}`))
}

func WithUser(ctx context.Context, u core.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// UserFrom returns the logged-in user; only valid behind Middleware.
func UserFrom(ctx context.Context) core.User {
	u, _ := ctx.Value(ctxKey{}).(core.User)
	return u
}

// Claims are the ID token claims we use.
type Claims struct {
	Subject           string `json:"sub"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	Email             string `json:"email"`
	ResourceAccess    map[string]struct {
		Roles []string `json:"roles"`
	} `json:"resource_access"`
}

// User maps claims to a user with the client roles of clientID.
func (c Claims) User(clientID string) core.User {
	username := c.PreferredUsername
	if username == "" {
		username = c.Subject
	}
	roles := c.ResourceAccess[clientID].Roles
	if roles == nil {
		roles = []string{}
	}
	return core.User{Username: username, Name: c.Name, Email: c.Email, Roles: roles}
}
