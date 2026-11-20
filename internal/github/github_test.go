package github

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var ctx = context.Background()

func TestVerifySignature(t *testing.T) {
	secret, body := []byte("s3cret"), []byte(`{"a":1}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !VerifySignature(secret, body, good) {
		t.Fatal("valid signature rejected")
	}
	for _, bad := range []string{"", "sha1=abc", "sha256=zz", good + "00"} {
		if VerifySignature(secret, body, bad) {
			t.Errorf("accepted %q", bad)
		}
	}
	if VerifySignature(nil, body, good) {
		t.Error("empty secret accepted")
	}
	if VerifySignature(secret, []byte(`{"a":2}`), good) {
		t.Error("tampered body accepted")
	}
}

type fakeGitHub struct {
	*httptest.Server
	key         *rsa.PrivateKey
	tokenCalls  atomic.Int32
	deployments []map[string]any
}

func newFake(t *testing.T) (*fakeGitHub, *App) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	f := &fakeGitHub{key: key}
	mux := http.NewServeMux()
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)

	mux.HandleFunc("POST /app/installations/42/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		f.tokenCalls.Add(1)
		if err := verifyJWT(&key.PublicKey, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); err != nil {
			http.Error(w, err.Error(), 401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"token": "inst-token", "expires_at": time.Now().Add(time.Hour)})
	})
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "token inst-token" {
				http.Error(w, "bad token", 401)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /repos/org/app/actions/runs/7/artifacts", auth(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"artifacts": []map[string]any{
			{"id": 1, "name": "cd-deploy", "expired": true},
			{"id": 2, "name": "cd-deploy", "expired": false},
		}})
	}))
	mux.HandleFunc("GET /repos/org/app/actions/artifacts/2/zip", auth(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/blob/2.zip", http.StatusFound)
	}))
	mux.HandleFunc("GET /blob/2.zip", func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		fw, _ := zw.Create("deploy.json")
		fw.Write([]byte(`{"version":1}`))
		zw.Close()
		w.Write(buf.Bytes())
	})
	mux.HandleFunc("GET /repos/org/app/contents/deploy/values%20test.yaml", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "abc" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": "base64", "size": 5,
			"content": base64.StdEncoding.EncodeToString([]byte("a: 1\n"))})
	}))
	mux.HandleFunc("GET /repos/org/app/git/ref/tags/v1.0", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":{"sha":"c1","type":"commit"}}`))
	}))
	mux.HandleFunc("GET /repos/org/app/git/ref/tags/v2.0", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":{"sha":"t2","type":"tag"}}`))
	}))
	mux.HandleFunc("GET /repos/org/app/git/tags/t2", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":{"sha":"c2","type":"commit"}}`))
	}))
	mux.HandleFunc("GET /repos/org/app/commits/abc/pulls", auth(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"number":3,"state":"closed"},{"number":4,"state":"open"}]`))
	}))
	mux.HandleFunc("POST /repos/org/app/deployments", auth(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.deployments = append(f.deployments, body)
		w.WriteHeader(201)
		w.Write([]byte(`{"id":99}`))
	}))
	app := &App{ID: 1234, Key: key, BaseURL: f.URL}
	return f, app
}

func verifyJWT(pub *rsa.PublicKey, tok string) error {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return errors.New("malformed")
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return err
	}
	var claims struct {
		Iss string `json:"iss"`
		Exp int64  `json:"exp"`
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	json.Unmarshal(raw, &claims)
	if claims.Iss != "1234" || claims.Exp < time.Now().Unix() {
		return errors.New("bad claims")
	}
	return nil
}

func TestClient(t *testing.T) {
	f, app := newFake(t)
	c := app.Client(42)

	data, err := c.DownloadArtifactFile(ctx, "org/app", 7, "cd-deploy", "deploy.json")
	if err != nil || string(data) != `{"version":1}` {
		t.Fatalf("artifact: %q, %v", data, err)
	}
	if _, err := c.DownloadArtifactFile(ctx, "org/app", 7, "cd-deploy", "other.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing file: %v", err)
	}
	if f.tokenCalls.Load() != 1 {
		t.Fatalf("token not cached: %d calls", f.tokenCalls.Load())
	}

	content, err := c.File(ctx, "org/app", "deploy/values test.yaml", "abc")
	if err != nil || string(content) != "a: 1\n" {
		t.Fatalf("file: %q, %v", content, err)
	}
	if _, err := c.File(ctx, "org/app", "deploy/values test.yaml", "other"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing ref: %v", err)
	}

	if sha, err := c.TagCommit(ctx, "org/app", "v1.0"); sha != "c1" || err != nil {
		t.Fatalf("TagCommit v1.0: %v %v", sha, err)
	}
	if sha, err := c.TagCommit(ctx, "org/app", "v2.0"); sha != "c2" || err != nil {
		t.Fatalf("TagCommit annotated v2.0: %v %v", sha, err)
	}
	if _, err := c.TagCommit(ctx, "org/app", "main"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TagCommit main: %v", err)
	}
	prs, err := c.PullRequestsForCommit(ctx, "org/app", "abc")
	if err != nil || len(prs) != 1 || prs[0] != 4 {
		t.Fatalf("prs = %v, %v", prs, err)
	}
	id, err := c.CreateDeployment(ctx, "org/app", DeploymentRequest{Ref: "abc", Environment: "app-test"})
	if err != nil || id != 99 || f.deployments[0]["environment"] != "app-test" || f.deployments[0]["auto_merge"] != false {
		t.Fatalf("deployment %d %v %v", id, err, f.deployments)
	}
}

func TestParsePrivateKey(t *testing.T) {
	if _, err := ParsePrivateKey([]byte("nope")); err == nil {
		t.Fatal("garbage accepted")
	}
}
