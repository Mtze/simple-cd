// Package api serves the JSON API, the GitHub webhook, the login endpoints
// and the embedded UI.
package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Mtze/simple-cd/internal/auth"
	"github.com/Mtze/simple-cd/internal/config"
	"github.com/Mtze/simple-cd/internal/core"
	"github.com/Mtze/simple-cd/internal/github"
	"github.com/Mtze/simple-cd/internal/ingest"
	"github.com/Mtze/simple-cd/internal/runner"
	"github.com/Mtze/simple-cd/internal/store"
)

// Authenticator is the part of auth.Auth the server uses (replaceable in tests).
type Authenticator interface {
	Login(http.ResponseWriter, *http.Request)
	Callback(http.ResponseWriter, *http.Request)
	Logout(http.ResponseWriter, *http.Request)
	Middleware(http.Handler) http.Handler
}

type Server struct {
	Store         *store.Store
	Config        *config.Holder
	Core          *core.Service
	Auth          Authenticator
	Ingest        *ingest.Ingester
	Runner        runner.Runner
	WebhookSecret []byte
	BaseURL       string // public URL, used for the Origin check and HSTS
	UI            fs.FS  // built UI; nil serves a placeholder
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	api := http.NewServeMux()

	api.HandleFunc("GET /api/me", s.me)
	api.HandleFunc("GET /api/projects", s.projects)
	api.HandleFunc("GET /api/projects/{owner}/{repo}", s.project)
	api.HandleFunc("GET /api/projects/{owner}/{repo}/artifacts", s.artifacts)
	api.HandleFunc("GET /api/artifacts/{id}", s.artifact)
	api.HandleFunc("GET /api/targets/{id}", s.target)
	api.HandleFunc("GET /api/targets/{id}/plan", s.plan)
	api.HandleFunc("POST /api/targets/{id}/deployments", s.deploy)
	api.HandleFunc("POST /api/targets/{id}/lock/release", s.releaseLock)
	api.HandleFunc("PUT /api/targets/{id}/maintenance", s.maintenance)
	api.HandleFunc("GET /api/deployments/{id}", s.deployment)
	api.HandleFunc("GET /api/deployments/{id}/log", s.deploymentLog)
	api.HandleFunc("DELETE /api/previews/{id}", s.teardownPreview)
	api.HandleFunc("GET /api/admin/audit", s.admin(s.audit))
	api.HandleFunc("GET /api/admin/previews", s.admin(s.allPreviews))
	api.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "not_found", "not found") })
	mux.Handle("/api/", noStore(s.csrf(s.Auth.Middleware(api))))

	mux.HandleFunc("GET /auth/login", s.Auth.Login)
	mux.HandleFunc("GET /auth/callback", s.Auth.Callback)
	mux.Handle("POST /auth/logout", s.csrf(http.HandlerFunc(s.Auth.Logout)))
	mux.HandleFunc("POST /webhooks/github", s.webhook)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", s.ready)
	mux.Handle("/", s.ui())
	return s.securityHeaders(mux)
}

// ---- middleware ----

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	hsts := strings.HasPrefix(s.BaseURL, "https://")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; "+
			"connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// csrf requires a custom header and a matching Origin on state-changing
// requests. Browsers cannot send the header cross-site without CORS, which we never allow.
func (s *Server) csrf(next http.Handler) http.Handler {
	origin := ""
	if u, err := url.Parse(s.BaseURL); err == nil {
		origin = u.Scheme + "://" + u.Host
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("X-Requested-With") != "simple-cd" {
				writeError(w, http.StatusForbidden, "csrf", "missing X-Requested-With header")
				return
			}
			if o := r.Header.Get("Origin"); o != "" && o != origin {
				writeError(w, http.StatusForbidden, "csrf", "cross-origin request rejected")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.UserFrom(r.Context()).IsAdmin() {
			writeError(w, http.StatusForbidden, "forbidden", "platform admins only")
			return
		}
		h(w, r)
	}
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string, extra ...any) {
	body := map[string]any{"error": msg, "code": code}
	for i := 0; i+1 < len(extra); i += 2 {
		body[extra[i].(string)] = extra[i+1]
	}
	writeJSON(w, status, body)
}

// fail maps domain errors to HTTP responses.
func fail(w http.ResponseWriter, err error) {
	var nd *core.NotDeployableError
	var le *core.LockedError
	var ce *core.ConfirmationError
	var ru *core.RegistryUnavailableError
	switch {
	case errors.As(err, &ru):
		writeError(w, 502, "registry_unavailable", err.Error())
	case errors.Is(err, core.ErrNotFound), errors.Is(err, store.ErrNotFound):
		writeError(w, 404, "not_found", "not found")
	case errors.Is(err, core.ErrForbidden):
		writeError(w, 403, "forbidden", err.Error())
	case errors.Is(err, core.ErrNeedReason):
		writeError(w, 400, "bad_request", err.Error())
	case errors.Is(err, core.ErrMaintenance):
		writeError(w, 409, "maintenance", err.Error())
	case errors.Is(err, core.ErrSlotBusy):
		writeError(w, 409, "slot_busy", "a deployment is already running here")
	case errors.As(err, &le):
		writeError(w, 409, "locked", err.Error(), "holder", le.Holder, "until", le.Until)
	case errors.As(err, &ce):
		writeError(w, 409, "confirmation_required", err.Error(), "diff", ce.Diff)
	case errors.As(err, &nd):
		writeError(w, 422, "not_deployable", "artifact is not deployable", "reasons", nd.Reasons)
	default:
		slog.Error("request failed", "err", err)
		writeError(w, 500, "internal", "internal error")
	}
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ---- handlers ----

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	out := map[string]any{"user": u, "isAdmin": u.IsAdmin()}
	if err := s.Config.LastError(); err != nil && u.IsAdmin() {
		out["configError"] = err.Error()
	}
	writeJSON(w, 200, out)
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), auth.UserFrom(r.Context())
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	previews, err := s.Store.LivePreviews(ctx, "")
	if err != nil {
		fail(w, err)
		return
	}
	names := snap.cfg.ProjectNames()
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	out := []map[string]any{}
	for _, name := range names {
		targets := []targetView{}
		for _, t := range snap.cfg.TargetsOf(name) {
			targets = append(targets, s.targetView(ctx, snap, u, t))
		}
		live := 0
		for _, p := range previews {
			if sameProject(p.Repo, name) && p.Status == store.PreviewActive {
				live++
			}
		}
		out = append(out, map[string]any{"name": name, "role": projectRole(u, name), "targets": targets, "livePreviews": live})
	}
	writeJSON(w, 200, out)
}

func (s *Server) projectName(r *http.Request) (string, bool) {
	return s.Config.Get().Project(r.PathValue("owner") + "/" + r.PathValue("repo"))
}

func (s *Server) project(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), auth.UserFrom(r.Context())
	name, ok := s.projectName(r)
	if !ok {
		fail(w, core.ErrNotFound)
		return
	}
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	targets := []targetView{}
	for _, t := range snap.cfg.TargetsOf(name) {
		targets = append(targets, s.targetView(ctx, snap, u, t))
	}
	list, err := s.Store.LivePreviews(ctx, name)
	if err != nil {
		fail(w, err)
		return
	}
	previews := []previewView{}
	for _, p := range list {
		previews = append(previews, s.previewView(snap.cfg, u, p))
	}
	writeJSON(w, 200, map[string]any{"name": name, "role": projectRole(u, name), "targets": targets, "previews": previews})
}

func (s *Server) artifacts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name, ok := s.projectName(r)
	if !ok {
		fail(w, core.ErrNotFound)
		return
	}
	q := r.URL.Query()
	source := q.Get("source")
	if source != "" && source != config.SourcePR && source != config.SourceBranch && source != config.SourceRelease {
		writeError(w, 400, "bad_request", "unknown source")
		return
	}
	list, err := s.Store.ListArtifacts(ctx, name, store.ArtifactFilter{Source: source, IncludeClosed: q.Get("closed") == "true"})
	if err != nil {
		fail(w, err)
		return
	}
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	out := []artifactView{}
	for _, a := range list {
		out = append(out, s.artifactView(snap, a))
	}
	writeJSON(w, 200, out)
}

func (s *Server) artifact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, core.ErrNotFound)
		return
	}
	a, err := s.Store.Artifact(ctx, id)
	if err != nil {
		fail(w, err)
		return
	}
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	list, err := s.Store.ArtifactDeployments(ctx, id)
	if err != nil {
		fail(w, err)
		return
	}
	deployments := []deploymentView{}
	for _, d := range list {
		deployments = append(deployments, s.deploymentView(ctx, snap, d))
	}
	writeJSON(w, 200, struct {
		artifactView
		Deployments []deploymentView `json:"deployments"`
	}{s.artifactView(snap, a), deployments})
}

func (s *Server) target(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), auth.UserFrom(r.Context())
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	t, ok := snap.cfg.Target(r.PathValue("id"))
	if !ok {
		fail(w, core.ErrNotFound)
		return
	}
	list, err := s.Store.TargetDeployments(ctx, t.ID, 50)
	if err != nil {
		fail(w, err)
		return
	}
	deployments := []deploymentView{}
	for _, d := range list {
		deployments = append(deployments, s.deploymentView(ctx, snap, d))
	}
	locks, err := s.Store.LockEvents(ctx, t.ID, 20)
	if err != nil {
		fail(w, err)
		return
	}
	if locks == nil {
		locks = []store.LockEvent{}
	}
	writeJSON(w, 200, map[string]any{"target": s.targetView(ctx, snap, u, t), "deployments": deployments, "lockHistory": locks})
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	artifact, err := strconv.ParseInt(r.URL.Query().Get("artifact"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "artifact query parameter required")
		return
	}
	p, err := s.Core.PlanDeploy(r.Context(), auth.UserFrom(r.Context()), r.PathValue("id"), artifact)
	if err != nil {
		fail(w, err)
		return
	}
	if p.Reasons == nil {
		p.Reasons = []string{}
	}
	writeJSON(w, 200, p)
}

func (s *Server) deploy(w http.ResponseWriter, r *http.Request) {
	var req core.DeployRequest
	if err := decode(r, &req); err != nil {
		writeError(w, 400, "bad_request", "invalid body")
		return
	}
	req.TargetID = r.PathValue("id")
	d, err := s.Core.Deploy(r.Context(), auth.UserFrom(r.Context()), req)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 201, d)
}

func (s *Server) releaseLock(w http.ResponseWriter, r *http.Request) {
	if err := s.Core.ReleaseLock(r.Context(), auth.UserFrom(r.Context()), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) maintenance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, "bad_request", "invalid body")
		return
	}
	if err := s.Core.SetMaintenance(r.Context(), auth.UserFrom(r.Context()), r.PathValue("id"), req.Enabled, req.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) deployment(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), auth.UserFrom(r.Context())
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, core.ErrNotFound)
		return
	}
	d, err := s.Store.Deployment(ctx, id)
	if err != nil {
		fail(w, err)
		return
	}
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	out := map[string]any{}
	b, _ := json.Marshal(s.deploymentView(ctx, snap, d))
	json.Unmarshal(b, &out)
	if t, ok := snap.cfg.Target(d.TargetID); ok {
		out["target"] = s.targetView(ctx, snap, u, t)
	}
	writeJSON(w, 200, out)
}

func finished(status string) bool {
	return status != store.StatusPending && status != store.StatusRunning
}

// deploymentLog streams the log as Server-Sent Events.
func (s *Server) deploymentLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, core.ErrNotFound)
		return
	}
	d, err := s.Store.Deployment(ctx, id)
	if err != nil {
		fail(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "internal", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func(event, data string) {
		for _, line := range strings.Split(data, "\n") {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, line)
		}
		flusher.Flush()
	}
	sendLog := func(text string) {
		for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", line)
		}
		flusher.Flush()
	}
	send("status", d.Status)

	// Wait for the worker to start the job.
	for d.Status == store.StatusPending {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		if d, err = s.Store.Deployment(ctx, id); err != nil {
			return
		}
		if d.Status != store.StatusPending {
			send("status", d.Status)
		}
	}
	if d.Status == store.StatusRunning && s.Runner != nil {
		if rc, err := s.Runner.Logs(ctx, d.JobName, true); err == nil {
			sc := bufio.NewScanner(rc)
			sc.Buffer(make([]byte, 64<<10), 1<<20)
			for sc.Scan() {
				send("log", sc.Text())
			}
			rc.Close()
		}
		// The worker stores the final status a moment after the job ends.
		for !finished(d.Status) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			if d, err = s.Store.Deployment(ctx, id); err != nil {
				return
			}
		}
		send("status", d.Status)
		send("end", "")
		return
	}
	if d.Log != nil && *d.Log != "" {
		sendLog(*d.Log)
	} else if finished(d.Status) {
		sendLog("(no log stored - it may have expired)")
	}
	if d.Error != "" {
		sendLog("error: " + d.Error)
	}
	send("status", d.Status)
	send("end", "")
}

func (s *Server) teardownPreview(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), auth.UserFrom(r.Context())
	id, ok := pathID(r, "id")
	if !ok {
		fail(w, core.ErrNotFound)
		return
	}
	p, err := s.Store.Preview(ctx, id)
	if err != nil {
		fail(w, err)
		return
	}
	if p.Status != store.PreviewActive {
		writeError(w, 409, "not_active", "preview is not active")
		return
	}
	if err := s.Core.TeardownPreview(ctx, &u, p); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(202)
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	events, err := s.Store.AuditEvents(r.Context(), before, 100)
	if err != nil {
		fail(w, err)
		return
	}
	if events == nil {
		events = []store.AuditEvent{}
	}
	writeJSON(w, 200, events)
}

func (s *Server) allPreviews(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), auth.UserFrom(r.Context())
	list, err := s.Store.LivePreviews(ctx, "")
	if err != nil {
		fail(w, err)
		return
	}
	cfg := s.Config.Get()
	out := []previewView{}
	for _, p := range list {
		out = append(out, s.previewView(cfg, u, p))
	}
	writeJSON(w, 200, out)
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Pool.Ping(ctx); err != nil {
		http.Error(w, "database unreachable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
}

// webhook verifies the signature, answers right away and processes in the background.
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		http.Error(w, "read error", 400)
		return
	}
	if !github.VerifySignature(s.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	var handle func(context.Context) error
	switch event {
	case "workflow_run":
		var ev github.WorkflowRunEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			http.Error(w, "bad payload", 400)
			return
		}
		handle = func(ctx context.Context) error { return s.Ingest.WorkflowRun(ctx, ev) }
	case "pull_request":
		var ev github.PullRequestEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			http.Error(w, "bad payload", 400)
			return
		}
		handle = func(ctx context.Context) error { return s.Ingest.PullRequest(ctx, ev) }
	default:
		w.WriteHeader(204) // ping, installation and other events need no action
		return
	}
	delivery := r.Header.Get("X-GitHub-Delivery")
	// shortcut: processing happens in memory after the 202; if this replica dies meanwhile, redeliver the event from the GitHub App settings.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := handle(ctx); err != nil {
			slog.Error("webhook processing failed", "event", event, "delivery", delivery, "err", err)
		}
	}()
	w.WriteHeader(202)
}

// ui serves the embedded single page app with a fallback to index.html.
func (s *Server) ui() http.Handler {
	if s.UI == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("simple-cd: UI not built (run make web)"))
		})
	}
	files := http.FileServerFS(s.UI)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(s.UI, p); err != nil {
			// Client-side route: let the app render it.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			p = "index.html"
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
