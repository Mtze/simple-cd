package registry

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExistsWithBearerChallenge(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			if r.URL.Query().Get("scope") != "repository:org/app:pull" {
				http.Error(w, "bad scope", 400)
				return
			}
			u, p, _ := r.BasicAuth()
			if u != "bot" || p != "pw" {
				http.Error(w, "no", 401)
				return
			}
			w.Write([]byte(`{"token":"t0k"}`))
		case r.Header.Get("Authorization") != "Bearer t0k":
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="reg",scope="repository:org/app:pull"`)
			w.WriteHeader(401)
		case r.Method == http.MethodHead && r.URL.Path == "/v2/org/app/manifests/sha256:aaa":
			if !strings.Contains(r.Header.Get("Accept"), "oci.image.index") {
				http.Error(w, "accept", 400)
				return
			}
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	c := &Checker{PlainHTTP: map[string]bool{host: true}, Credentials: map[string]Credential{host: {"bot", "pw"}}}

	ok, err := c.Exists(context.Background(), host+"/org/app", "sha256:aaa")
	if err != nil || !ok {
		t.Fatalf("existing: %v %v", ok, err)
	}
	ok, err = c.Exists(context.Background(), host+"/org/app", "sha256:bbb")
	if err != nil || ok {
		t.Fatalf("missing: %v %v", ok, err)
	}
	c.Credentials = nil
	if _, err := c.Exists(context.Background(), host+"/org/app", "sha256:aaa"); !errors.Is(err, ErrDenied) {
		t.Fatalf("without credentials: %v", err)
	}
}

func TestParseChallenge(t *testing.T) {
	s, p := parseChallenge(`Bearer realm="https://ghcr.io/token",service="ghcr.io",scope="repository:a/b:pull,push"`)
	if s != "Bearer" || p["realm"] != "https://ghcr.io/token" || p["scope"] != "repository:a/b:pull,push" || p["service"] != "ghcr.io" {
		t.Fatalf("%s %v", s, p)
	}
}

func TestLoadDockerConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	auth := base64.StdEncoding.EncodeToString([]byte("user:pa:ss"))
	os.WriteFile(path, []byte(`{"auths":{"https://ghcr.io/":{"auth":"`+auth+`"},"harbor.local":{"username":"u","password":"p"}}}`), 0o600)
	c, err := LoadDockerConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if c["ghcr.io"] != (Credential{"user", "pa:ss"}) || c["harbor.local"] != (Credential{"u", "p"}) {
		t.Fatalf("%v", c)
	}
}

func TestRedirectToOtherHostNotFollowed(t *testing.T) {
	hits := 0
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer internal.Close()
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/secret", http.StatusFound)
	}))
	defer reg.Close()
	host := strings.TrimPrefix(reg.URL, "http://")
	c := &Checker{PlainHTTP: map[string]bool{host: true}}
	if _, err := c.Exists(context.Background(), host+"/org/app", "sha256:aaa"); err == nil {
		t.Fatal("redirect to another host accepted")
	}
	if hits != 0 {
		t.Fatalf("internal host received %d requests", hits)
	}
}

func TestTokenRealmMustBeHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="http://127.0.0.1:1/token"`)
		w.WriteHeader(401)
	}))
	defer srv.Close()
	c := &Checker{HTTP: srv.Client()}
	_, err := c.Exists(context.Background(), strings.TrimPrefix(srv.URL, "https://")+"/org/app", "sha256:aaa")
	if err == nil || !strings.Contains(err.Error(), "not https") {
		t.Fatalf("http realm: %v", err)
	}
}
