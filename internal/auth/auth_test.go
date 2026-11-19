package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Mtze/simple-cd/internal/store/storetest"
)

// fakeKeycloak is a minimal OIDC provider: discovery, JWKS and a token
// endpoint that checks the PKCE verifier and returns a signed ID token.
type fakeKeycloak struct {
	*httptest.Server
	key       *rsa.PrivateKey
	nonce     string
	challenge string
}

func newKeycloak(t *testing.T) *fakeKeycloak {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	k := &fakeKeycloak{key: key}
	mux := http.NewServeMux()
	k.Server = httptest.NewServer(mux)
	t.Cleanup(k.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": k.URL, "authorization_endpoint": k.URL + "/auth", "token_endpoint": k.URL + "/token",
			"jwks_uri": k.URL + "/certs", "end_session_endpoint": k.URL + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/certs", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if r.Form.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != k.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 300,
			"id_token": k.sign(map[string]any{
				"iss": k.URL, "aud": "simple-cd", "sub": "u-1", "exp": time.Now().Add(time.Hour).Unix(),
				"iat": time.Now().Unix(), "nonce": k.nonce, "preferred_username": "alice", "name": "Alice",
				"email": "alice@example.org",
				"resource_access": map[string]any{
					"simple-cd": map[string]any{"roles": []string{"project:org/app:developer"}},
					"other":     map[string]any{"roles": []string{"ignored"}},
				},
			}),
		})
	})
	return k
}

func (k *fakeKeycloak) sign(claims map[string]any) string {
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k1","typ":"JWT"}`))
	c, _ := json.Marshal(claims)
	in := h + "." + base64.RawURLEncoding.EncodeToString(c)
	sum := sha256.Sum256([]byte(in))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, k.key, crypto.SHA256, sum[:])
	return in + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestLoginFlow(t *testing.T) {
	kc := newKeycloak(t)
	s := storetest.New(t)
	a, err := New(context.Background(), Config{Issuer: kc.URL, ClientID: "simple-cd", ClientSecret: "x", BaseURL: "https://cd.example.org"}, s)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Login redirects to Keycloak with PKCE and remembers the return path.
	rec := httptest.NewRecorder()
	a.Login(rec, httptest.NewRequest("GET", "/auth/login?return=/projects/org/app", nil))
	loc, _ := url.Parse(rec.Header().Get("Location"))
	q := loc.Query()
	if !strings.HasPrefix(loc.String(), kc.URL+"/auth") || q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != "https://cd.example.org/auth/callback" {
		t.Fatalf("redirect = %s", loc)
	}
	kc.nonce, kc.challenge = q.Get("nonce"), q.Get("code_challenge")

	// 2. Callback creates a session cookie and returns to the page.
	rec = httptest.NewRecorder()
	a.Callback(rec, httptest.NewRequest("GET", "/auth/callback?code=good-code&state="+q.Get("state"), nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/projects/org/app" {
		t.Fatalf("callback = %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	cookie := rec.Result().Cookies()[0]
	if cookie.Name != "__Host-simplecd_session" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v", cookie)
	}

	// 3. The state is single use.
	rec = httptest.NewRecorder()
	a.Callback(rec, httptest.NewRequest("GET", "/auth/callback?code=good-code&state="+q.Get("state"), nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("replayed state: %d", rec.Code)
	}

	// 4. The middleware resolves the user with client roles only.
	var got string
	h := a.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := UserFrom(r.Context())
		got = u.Username + "|" + strings.Join(u.Roles, ",")
	}))
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got != "alice|project:org/app:developer" {
		t.Fatalf("user = %q", got)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/me", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie: %d", rec.Code)
	}

	// 5. Logout deletes the session and points to Keycloak with the hint.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/auth/logout", nil)
	req.AddCookie(cookie)
	a.Logout(rec, req)
	var out map[string]string
	json.NewDecoder(rec.Body).Decode(&out)
	lu, _ := url.Parse(out["logoutUrl"])
	if !strings.HasPrefix(out["logoutUrl"], kc.URL+"/logout") || lu.Query().Get("id_token_hint") == "" || lu.Query().Get("post_logout_redirect_uri") != "https://cd.example.org/" {
		t.Fatalf("logout = %s", out["logoutUrl"])
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatal("session still valid after logout")
	}
}

func TestCallbackRejectsWrongNonce(t *testing.T) {
	kc := newKeycloak(t)
	s := storetest.New(t)
	a, _ := New(context.Background(), Config{Issuer: kc.URL, ClientID: "simple-cd", BaseURL: "http://localhost:8080"}, s)
	rec := httptest.NewRecorder()
	a.Login(rec, httptest.NewRequest("GET", "/auth/login", nil))
	loc, _ := url.Parse(rec.Header().Get("Location"))
	kc.nonce, kc.challenge = "other-nonce", loc.Query().Get("code_challenge")
	rec = httptest.NewRecorder()
	a.Callback(rec, httptest.NewRequest("GET", "/auth/callback?code=good-code&state="+loc.Query().Get("state"), nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong nonce accepted: %d", rec.Code)
	}
}

func TestSafeReturn(t *testing.T) {
	cases := map[string]string{
		"/projects/x": "/projects/x", "": "/", "https://evil.example": "/", "//evil.example": "/",
		"/\\evil.example": "/", "/auth/logout": "/",
	}
	for in, want := range cases {
		if got := safeReturn(in); got != want {
			t.Errorf("safeReturn(%q) = %q", in, got)
		}
	}
}
