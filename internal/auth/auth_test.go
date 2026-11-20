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
	username  string
	roles     []string
}

func newKeycloak(t *testing.T) *fakeKeycloak {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	k := &fakeKeycloak{key: key, username: "alice", roles: []string{"project:org/app:developer"}}
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
				"iat": time.Now().Unix(), "nonce": k.nonce, "preferred_username": k.username, "name": "Alice",
				"email": "alice@example.org",
				"resource_access": map[string]any{
					"simple-cd": map[string]any{"roles": k.roles},
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

// startLogin calls Login and returns the login cookie and Keycloak's redirect.
func startLogin(t *testing.T, a *Auth, kc *fakeKeycloak, target string) (*http.Cookie, url.Values) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.Login(rec, httptest.NewRequest("GET", target, nil))
	loc, _ := url.Parse(rec.Header().Get("Location"))
	q := loc.Query()
	kc.nonce, kc.challenge = q.Get("nonce"), q.Get("code_challenge")
	return rec.Result().Cookies()[0], q
}

// resume points the fake Keycloak back at the login in cookie c, after
// another login replaced its nonce and PKCE challenge.
func (k *fakeKeycloak) resume(t *testing.T, c *http.Cookie) {
	raw, _ := base64.RawURLEncoding.DecodeString(c.Value)
	var ls loginState
	if err := json.Unmarshal(raw, &ls); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(ls.Verifier))
	k.nonce, k.challenge = ls.Nonce, base64.RawURLEncoding.EncodeToString(sum[:])
}

func callback(a *Auth, state string, cookie *http.Cookie) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/auth/callback?code=good-code&state="+state, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	a.Callback(rec, req)
	return rec
}

func TestLoginFlow(t *testing.T) {
	kc := newKeycloak(t)
	s := storetest.New(t)
	a, err := New(context.Background(), Config{Issuer: kc.URL, ClientID: "simple-cd", ClientSecret: "x", BaseURL: "https://cd.example.org"}, s)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Login redirects to Keycloak with PKCE and keeps the state in a cookie.
	loginCookie, q := startLogin(t, a, kc, "/auth/login?return=/projects/org/app")
	if q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != "https://cd.example.org/auth/callback" {
		t.Fatalf("redirect = %v", q)
	}
	if !strings.HasPrefix(loginCookie.Name, "__Host-simplecd_login-") || !loginCookie.HttpOnly || !loginCookie.Secure || loginCookie.MaxAge != 600 {
		t.Fatalf("login cookie = %+v", loginCookie)
	}

	// 2. Another browser cannot finish this login (login CSRF).
	if rec := callback(a, q.Get("state"), nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("callback without login cookie: %d", rec.Code)
	}
	otherCookie, _ := startLogin(t, a, kc, "/auth/login")
	otherCookie.Name = loginCookie.Name
	if rec := callback(a, q.Get("state"), otherCookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("callback with another login's cookie: %d", rec.Code)
	}

	// 3. Callback creates a session cookie, clears the login cookie and returns to the page,
	// also while another tab has started its own login.
	loginCookie, q = startLogin(t, a, kc, "/auth/login?return=/projects/org/app")
	startLogin(t, a, kc, "/auth/login")
	kc.resume(t, loginCookie)
	rec := callback(a, q.Get("state"), loginCookie)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/projects/org/app" {
		t.Fatalf("callback = %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	cookies := rec.Result().Cookies()
	if cookies[0].Name != loginCookie.Name || cookies[0].MaxAge >= 0 {
		t.Fatalf("login cookie not cleared: %+v", cookies[0])
	}
	cookie := cookies[1]
	if cookie.Name != "__Host-simplecd_session" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v", cookie)
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
	c, q := startLogin(t, a, kc, "/auth/login")
	kc.nonce = "other-nonce"
	if rec := callback(a, q.Get("state"), c); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong nonce accepted: %d", rec.Code)
	}
}

func TestCallbackRefusesUsersWithoutAccess(t *testing.T) {
	kc := newKeycloak(t)
	a, _ := New(context.Background(), Config{Issuer: kc.URL, ClientID: "simple-cd", BaseURL: "http://localhost:8080"}, storetest.New(t))
	for name, set := range map[string]func(){
		"no role":       func() { kc.username, kc.roles = "guest", []string{} },
		"reserved name": func() { kc.username, kc.roles = "auto-deploy", []string{"platform-admin"} },
	} {
		set()
		c, q := startLogin(t, a, kc, "/auth/login")
		if rec := callback(a, q.Get("state"), c); rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
}

func TestSafeReturn(t *testing.T) {
	cases := map[string]string{
		"/projects/x": "/projects/x", "": "/", "https://evil.example": "/", "//evil.example": "/",
		"/\\evil.example": "/", "/auth/logout": "/", "/auth": "/", "/x/../auth/callback": "/",
		"/\t/evil.example": "/", "/x/../\\evil.example": "/", "/x/..//evil.example": "/evil.example",
		"/projects/x?tab=branches": "/projects/x?tab=branches", "/authors": "/authors",
	}
	for in, want := range cases {
		if got := safeReturn(in); got != want {
			t.Errorf("safeReturn(%q) = %q", in, got)
		}
	}
}
