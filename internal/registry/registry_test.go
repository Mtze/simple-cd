package registry

import (
	"context"
	"encoding/base64"
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
	if _, err := c.Exists(context.Background(), host+"/org/app", "sha256:aaa"); err == nil {
		t.Fatal("expected token error without credentials")
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
