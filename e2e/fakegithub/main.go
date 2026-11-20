// Command fakegithub serves the part of the GitHub API simple-cd calls, for
// the end-to-end test. The test registers workflow runs with POST /_e2e/runs
// and reads the deployment statuses simple-cd reported from GET /_e2e/statuses.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Run is one workflow run with the repository state at its commit.
type Run struct {
	Repo       string            `json:"repo"`
	ID         int64             `json:"id"`
	SHA        string            `json:"sha"`
	DeployJSON string            `json:"deployJson"`
	Files      map[string]string `json:"files"` // path -> content at SHA
	Pulls      []int             `json:"pulls"` // open PRs containing SHA
}

type Status struct {
	Repo        string `json:"repo"`
	Deployment  int64  `json:"deployment"`
	Environment string `json:"environment"`
	Ref         string `json:"ref"`
	State       string `json:"state"`
}

type fake struct {
	mu       sync.Mutex
	runs     map[int64]Run
	envs     map[int64][2]string // deployment id -> environment, ref
	statuses []Status
}

func main() {
	f := &fake{runs: map[int64]Run{}, envs: map[int64][2]string{}}
	log.Println("fake GitHub listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", f.routes()))
}

func (f *fake) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /_e2e/runs", func(w http.ResponseWriter, r *http.Request) {
		var run Run
		if err := json.NewDecoder(r.Body).Decode(&run); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		f.mu.Lock()
		f.runs[run.ID] = run
		f.mu.Unlock()
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /_e2e/statuses", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		writeJSON(w, f.statuses)
	})
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"token": "fake-installation-token", "expires_at": time.Now().Add(time.Hour)})
	})
	mux.HandleFunc("GET /repos/{owner}/{repo}/installation", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"id": 1})
	})
	mux.HandleFunc("GET /repos/{owner}/{repo}/actions/runs/{run}/artifacts", func(w http.ResponseWriter, r *http.Request) {
		run, ok := f.run(r.PathValue("run"))
		if !ok || run.DeployJSON == "" {
			writeJSON(w, map[string]any{"artifacts": []any{}})
			return
		}
		writeJSON(w, map[string]any{"artifacts": []any{map[string]any{"id": run.ID, "name": r.URL.Query().Get("name"), "expired": false}}})
	})
	mux.HandleFunc("GET /repos/{owner}/{repo}/actions/artifacts/{id}/zip", func(w http.ResponseWriter, r *http.Request) {
		run, ok := f.run(r.PathValue("id"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		fw, _ := zw.Create("deploy.json")
		fw.Write([]byte(run.DeployJSON))
		zw.Close()
		w.Write(buf.Bytes())
	})
	mux.HandleFunc("GET /repos/{owner}/{repo}/contents/{path...}", func(w http.ResponseWriter, r *http.Request) {
		content, ok := f.file(r.PathValue("owner")+"/"+r.PathValue("repo"), r.URL.Query().Get("ref"), r.PathValue("path"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"type": "file", "encoding": "base64", "size": len(content),
			"content": base64.StdEncoding.EncodeToString([]byte(content))})
	})
	mux.HandleFunc("GET /repos/{owner}/{repo}/commits/{sha}/pulls", func(w http.ResponseWriter, r *http.Request) {
		var out []map[string]int
		f.mu.Lock()
		for _, run := range f.runs {
			if run.SHA == r.PathValue("sha") {
				for _, n := range run.Pulls {
					out = append(out, map[string]int{"number": n})
				}
			}
		}
		f.mu.Unlock()
		writeJSON(w, out)
	})
	mux.HandleFunc("GET /repos/{owner}/{repo}/git/ref/tags/{name...}", http.NotFound)
	mux.HandleFunc("POST /repos/{owner}/{repo}/deployments", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Ref         string `json:"ref"`
			Environment string `json:"environment"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		id := int64(len(f.envs) + 1)
		f.envs[id] = [2]string{req.Environment, req.Ref}
		f.mu.Unlock()
		writeJSON(w, map[string]any{"id": id})
	})
	mux.HandleFunc("POST /repos/{owner}/{repo}/deployments/{id}/statuses", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			State string `json:"state"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		f.mu.Lock()
		env := f.envs[id]
		f.statuses = append(f.statuses, Status{Repo: r.PathValue("owner") + "/" + r.PathValue("repo"),
			Deployment: id, Environment: env[0], Ref: env[1], State: req.State})
		f.mu.Unlock()
		writeJSON(w, map[string]any{"id": len(f.statuses)})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("unhandled %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	})
	return mux
}

func (f *fake) run(id string) (Run, bool) {
	n, _ := strconv.ParseInt(id, 10, 64)
	f.mu.Lock()
	defer f.mu.Unlock()
	run, ok := f.runs[n]
	return run, ok
}

func (f *fake) file(repo, ref, path string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, run := range f.runs {
		if strings.EqualFold(run.Repo, repo) && run.SHA == ref {
			content, ok := run.Files[path]
			return content, ok
		}
	}
	return "", false
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
