package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Mtze/simple-cd/internal/auth"
	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/core"
	"github.com/Mtze/simple-cd/internal/ingest"
	"github.com/Mtze/simple-cd/internal/repoconfig"
	"github.com/Mtze/simple-cd/internal/store"
	"github.com/Mtze/simple-cd/internal/store/storetest"
)

const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var ctx = context.Background()

// fakeAuth takes the user from a test header instead of a session cookie.
type fakeAuth struct{}

func (fakeAuth) Login(w http.ResponseWriter, r *http.Request)    { w.WriteHeader(302) }
func (fakeAuth) Callback(w http.ResponseWriter, r *http.Request) { w.WriteHeader(302) }
func (fakeAuth) Logout(w http.ResponseWriter, r *http.Request)   { w.Write([]byte(`{"logoutUrl":"/"}`)) }
func (fakeAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("X-Test-User")
		if raw == "" {
			w.WriteHeader(401)
			return
		}
		var u core.User
		json.Unmarshal([]byte(raw), &u)
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), u)))
	})
}

type okRegistry struct{}

func (okRegistry) Exists(context.Context, string, string) (bool, error) { return true, nil }

var (
	dev   = core.User{Username: "alice", Roles: []string{"project:org/app:developer"}}
	other = core.User{Username: "bob", Roles: []string{"project:org/app:developer"}}
	admin = core.User{Username: "root", Roles: []string{core.RolePlatformAdmin}}
	guest = core.User{Username: "guest"}
)

type env struct {
	srv *httptest.Server
	s   *Server
	a   *store.Artifact
}

func setup(t *testing.T) *env {
	cfg, err := config.Parse([]byte(`
defaults: {registries: [ghcr.io/org/]}
targets:
  - {id: app-test, project: org/app, type: helm, lockable: true, cluster: "vault:c", namespace: app}
  - {id: app-prod, project: org/app, type: helm, role: deploy:app-prod, cluster: "vault:c", namespace: app}
`))
	if err != nil {
		t.Fatal(err)
	}
	st := storetest.New(t)
	holder := config.NewStaticHolder(cfg)
	svc := &core.Service{Store: st, Config: holder, Registry: okRegistry{}}
	s := &Server{
		Store: st, Config: holder, Core: svc, Auth: fakeAuth{}, WebhookSecret: []byte("hook"),
		BaseURL: "https://cd.example.org",
		Ingest:  &ingest.Ingester{Config: holder, Store: st, Core: svc},
		UI:      fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}, "assets/app.js": {Data: []byte("js")}},
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	a, _, err := st.UpsertArtifact(ctx, &store.Artifact{
		Repo: "org/app", SHA: "abc1234", Source: "branch", RefName: "main", RunID: 1,
		Manifest:   repoconfig.Manifest{Version: 1, Images: []repoconfig.Image{{Name: "app", Ref: "ghcr.io/org/app@" + digest}}, Helm: &repoconfig.ManifestHelm{Version: "1.0.0"}},
		RepoConfig: &repoconfig.RepoConfig{Helm: &repoconfig.HelmConfig{Chart: "oci://ghcr.io/org/charts/app"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &env{srv: srv, s: s, a: a}
}

func (e *env) do(t *testing.T, u *core.User, method, path string, body any) (*http.Response, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if u != nil {
		b, _ := json.Marshal(u)
		req.Header.Set("X-Test-User", string(b))
	}
	if method != http.MethodGet {
		req.Header.Set("X-Requested-With", "simple-cd")
		req.Header.Set("Origin", "https://cd.example.org")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	json.Unmarshal(raw, &out)
	return resp, out
}

func TestUnauthenticatedAndSecurityHeaders(t *testing.T) {
	e := setup(t)
	resp, _ := e.do(t, nil, "GET", "/api/me", nil)
	if resp.StatusCode != 401 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") ||
		resp.Header.Get("X-Frame-Options") != "DENY" || resp.Header.Get("Strict-Transport-Security") == "" ||
		resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", resp.Header)
	}
}

func TestMeAndProjectsPermissions(t *testing.T) {
	e := setup(t)
	_, me := e.do(t, &admin, "GET", "/api/me", nil)
	if me["isAdmin"] != true {
		t.Fatalf("me = %v", me)
	}

	req, _ := http.NewRequest("GET", e.srv.URL+"/api/projects", nil)
	b, _ := json.Marshal(dev)
	req.Header.Set("X-Test-User", string(b))
	resp, _ := http.DefaultClient.Do(req)
	var projects []struct {
		Name    string
		Role    string
		Targets []struct {
			ID          string
			Status      string
			Permissions core.Permissions
		}
	}
	json.NewDecoder(resp.Body).Decode(&projects)
	resp.Body.Close()
	if len(projects) != 1 || projects[0].Role != "developer" {
		t.Fatalf("projects = %+v", projects)
	}
	perms := map[string]core.Permissions{}
	for _, tv := range projects[0].Targets {
		perms[tv.ID] = tv.Permissions
	}
	if !perms["app-test"].Deploy || perms["app-prod"].Deploy || perms["app-test"].Override {
		t.Fatalf("permissions = %+v", perms)
	}
}

func TestDeployFlowAndErrorCodes(t *testing.T) {
	e := setup(t)
	path := "/api/targets/app-test/deployments"
	if resp, _ := e.do(t, &guest, "POST", path, map[string]any{"artifactId": e.a.ID}); resp.StatusCode != 403 {
		t.Fatalf("guest: %d", resp.StatusCode)
	}
	resp, body := e.do(t, &dev, "POST", path, map[string]any{"artifactId": e.a.ID})
	if resp.StatusCode != 201 || body["status"] != "pending" {
		t.Fatalf("deploy: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, &dev, "POST", path, map[string]any{"artifactId": e.a.ID}); resp.StatusCode != 409 || body["code"] != "slot_busy" {
		t.Fatalf("busy: %d %v", resp.StatusCode, body)
	}
	e.s.Store.Finish(ctx, int64(body["id"].(float64)), store.StatusSucceeded, "line 1\nline 2\n", "")
	if resp, body := e.do(t, &other, "POST", path, map[string]any{"artifactId": e.a.ID}); resp.StatusCode != 409 || body["code"] != "locked" || body["holder"] != "alice" {
		t.Fatalf("locked: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, &dev, "POST", path, map[string]any{"artifactId": e.a.ID, "typo": 1}); resp.StatusCode != 400 {
		t.Fatalf("unknown field: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(t, &dev, "GET", "/api/targets/app-test/plan?artifact="+itoa(e.a.ID), nil)
	if resp.StatusCode != 200 || body["status"] != "locked" || body["needsOverride"] != false {
		t.Fatalf("plan: %v", body)
	}
	_, target := e.do(t, &dev, "GET", "/api/targets/app-test", nil)
	tv := target["target"].(map[string]any)
	if tv["lock"].(map[string]any)["holder"] != "alice" || tv["current"].(map[string]any)["sha"] != "abc1234" {
		t.Fatalf("target = %v", tv)
	}
	if resp, _ := e.do(t, &dev, "PUT", "/api/targets/app-test/maintenance", map[string]any{"enabled": true, "reason": "x"}); resp.StatusCode != 403 {
		t.Fatalf("developer maintenance: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, &admin, "PUT", "/api/targets/app-test/maintenance", map[string]any{"enabled": true}); resp.StatusCode != 400 {
		t.Fatalf("missing reason: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, &dev, "POST", "/api/targets/app-test/lock/release", nil); resp.StatusCode != 204 {
		t.Fatalf("release: %d", resp.StatusCode)
	}
}

func TestCSRF(t *testing.T) {
	e := setup(t)
	b, _ := json.Marshal(dev)
	for _, hdr := range []map[string]string{
		{},
		{"X-Requested-With": "simple-cd", "Origin": "https://evil.example"},
	} {
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/targets/app-test/lock/release", nil)
		req.Header.Set("X-Test-User", string(b))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatalf("headers %v: %d", hdr, resp.StatusCode)
		}
	}
}

func TestAdminOnly(t *testing.T) {
	e := setup(t)
	if resp, _ := e.do(t, &dev, "GET", "/api/admin/audit", nil); resp.StatusCode != 403 {
		t.Fatalf("dev audit: %d", resp.StatusCode)
	}
	e.s.Store.Audit(ctx, "x", "y", "z", nil)
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/admin/audit", nil)
	b, _ := json.Marshal(admin)
	req.Header.Set("X-Test-User", string(b))
	resp, _ := http.DefaultClient.Do(req)
	var events []map[string]any
	json.NewDecoder(resp.Body).Decode(&events)
	resp.Body.Close()
	if len(events) != 1 {
		t.Fatalf("events = %v", events)
	}
}

func TestLogStreamForFinishedDeployment(t *testing.T) {
	e := setup(t)
	d, _ := e.s.Store.InsertDeployment(ctx, &store.Deployment{ArtifactID: &e.a.ID, TargetID: "app-test", Actor: "alice"})
	// A lone \r in the log must not start an SSE field such as "event: status".
	e.s.Store.Finish(ctx, d.ID, store.StatusFailed, "==> helm\nboom\revent: status\n", "exit 1")
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/deployments/"+itoa(d.ID)+"/log", nil)
	b, _ := json.Marshal(dev)
	req.Header.Set("X-Test-User", string(b))
	resp, _ := http.DefaultClient.Do(req)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	want := "event: status\ndata: failed\n\nevent: log\ndata: ==> helm\n\nevent: log\ndata: boom\n\nevent: log\ndata: event: status\n\nevent: log\ndata: error: exit 1\n\nevent: status\ndata: failed\n\nevent: end\ndata: \n\n"
	if resp.Header.Get("Content-Type") != "text/event-stream" || string(raw) != want {
		t.Fatalf("stream = %q", raw)
	}
}

func TestWebhookSignature(t *testing.T) {
	e := setup(t)
	body := []byte(`{"zen":"hi"}`)
	send := func(sig string) int {
		req, _ := http.NewRequest("POST", e.srv.URL+"/webhooks/github", bytes.NewReader(body))
		req.Header.Set("X-GitHub-Event", "ping")
		req.Header.Set("X-Hub-Signature-256", sig)
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
		return resp.StatusCode
	}
	mac := hmac.New(sha256.New, []byte("hook"))
	mac.Write(body)
	if code := send("sha256=" + hex.EncodeToString(mac.Sum(nil))); code != 204 {
		t.Fatalf("valid: %d", code)
	}
	if code := send("sha256=00"); code != 401 {
		t.Fatalf("invalid: %d", code)
	}
}

func TestUIFallback(t *testing.T) {
	e := setup(t)
	for path, want := range map[string]string{"/": "<html>app</html>", "/projects/org/app": "<html>app</html>", "/assets/app.js": "js"} {
		resp, err := http.Get(e.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(raw) != want {
			t.Errorf("%s = %q", path, raw)
		}
	}
	resp, _ := http.Get(e.srv.URL + "/api/nope")
	if resp.StatusCode != 401 { // the API never falls back to the UI
		t.Errorf("api path: %d", resp.StatusCode)
	}
}

func itoa(i int64) string { return strings.TrimSpace(jsonNumber(i)) }

func jsonNumber(i int64) string { b, _ := json.Marshal(i); return string(b) }
