//go:build e2e

// Package e2e drives a complete simple-cd installation in a kind cluster:
// GitHub webhooks, the registry check, the Keycloak login, a deploy through an
// executor Job that reads its secrets from Vault, and a preview from pull
// request to teardown. Start the environment with `make e2e-up` first.
package e2e

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	server        = "http://127.0.0.1:18080"
	fakeGitHub    = "http://127.0.0.1:18082"
	registry      = "http://127.0.0.1:5001" // kind-registry:5000 inside the cluster
	webhookSecret = "e2e-webhook-secret"
	repo          = "example/demo"
	kubeContext   = "kind-simple-cd-e2e"
	chart         = "oci://kind-registry:5000/e2e/charts/demo"
)

const repoConfig = `helm:
  chart: ` + chart + `
  set:
    image.repository: "{images.app.repository}"
    image.digest: "{images.app.digest}"
  targets:
    e2e-test:
      values: deploy/values-test.yaml
    e2e-previews:
      set:
        message: "pr-{cd.pr}"
`

// The issuer must look the same to simple-cd in the cluster and to this test,
// so the test reaches http://keycloak:18081 through the kind port mapping.
func newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	var d net.Dialer
	return &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == "keycloak:18081" {
				addr = "127.0.0.1:18081"
			}
			return d.DialContext(ctx, network, addr)
		}},
	}
}

func TestDelivery(t *testing.T) {
	stamp := time.Now().UnixNano()
	sha := func(n int) string { return fmt.Sprintf("%038x%02d", stamp, n) }
	runID := func(n int) int64 { return stamp/1e6 + int64(n) }
	image := "kind-registry:5000/e2e/demo@" + digest(t, "e2e/demo", "1")
	pr := int(stamp%1000) + 1
	previewNS := fmt.Sprintf("pr-demo-%d", pr)

	c := newClient()
	login(t, c, "alice")

	var mainArtifact int64
	t.Run("ingest a main build", func(t *testing.T) {
		publish(t, run{ID: runID(1), SHA: sha(1), Image: image, Event: "push", Branch: "main"})
		a := waitArtifact(t, c, sha(1))
		if r := a.Readiness["e2e-test"]; len(r) != 0 {
			t.Fatalf("artifact not deployable to e2e-test: %v", r)
		}
		mainArtifact = a.ID
	})

	t.Run("registry check finds a missing image", func(t *testing.T) {
		missing := "kind-registry:5000/e2e/demo@sha256:" + strings.Repeat("0", 64)
		publish(t, run{ID: runID(2), SHA: sha(2), Image: missing, Event: "push", Branch: "main"})
		a := waitArtifact(t, c, sha(2))
		if !slices.ContainsFunc(a.Readiness["e2e-test"], func(r string) bool { return strings.Contains(r, "missing in registry") }) {
			t.Fatalf("want a missing-image reason, got %v", a.Readiness)
		}
	})

	t.Run("deploy with secrets from Vault", func(t *testing.T) {
		if mainArtifact == 0 {
			t.Skip("no artifact")
		}
		var d struct{ ID int64 }
		api(t, c, "POST", "/api/targets/e2e-test/deployments", map[string]any{"artifactId": mainArtifact, "confirm": true}, &d, 201)
		status, log := followLog(t, c, d.ID)
		if status != "succeeded" {
			t.Fatalf("deployment %d %s:\n%s", d.ID, status, log)
		}
		for _, want := range []string{"Logging in to Vault", "Reading cluster credentials", "helm upgrade --install demo " + chart} {
			if !strings.Contains(log, want) {
				t.Errorf("log lacks %q:\n%s", want, log)
			}
		}
		cm := configMap(t, "e2e-app")
		want := map[string]string{"message": "from the repository", "greeting": "hello from vault", "image": image}
		for k, v := range want {
			if cm[k] != v {
				t.Errorf("configmap %s = %q, want %q", k, cm[k], v)
			}
		}
		waitStatus(t, "e2e-test", sha(1), "success")
	})

	t.Run("preview from pull request to teardown", func(t *testing.T) {
		publish(t, run{ID: runID(3), SHA: sha(3), Image: image, Event: "pull_request", Branch: "feature", PR: pr,
			AutoDeploy: []string{"e2e-previews"}})
		env := fmt.Sprintf("e2e-previews/pr-%d", pr)
		waitStatus(t, env, sha(3), "success")

		labels := kubectlJSON[struct {
			Metadata struct{ Labels map[string]string }
		}](t, "get", "namespace", previewNS)
		if l := labels.Metadata.Labels; l["cd/managed"] != "true" || l["cd/target"] != "e2e-previews" || l["cd/pr"] != fmt.Sprint(pr) {
			t.Errorf("preview namespace labels %v", l)
		}
		if m := configMap(t, previewNS)["message"]; m != fmt.Sprintf("pr-%d", pr) {
			t.Errorf("preview message %q", m)
		}

		webhook(t, "pull_request", map[string]any{"action": "closed", "number": pr,
			"repository": map[string]any{"full_name": repo}, "installation": map[string]any{"id": 1}})
		waitStatus(t, env, sha(3), "inactive")
		eventually(t, 2*time.Minute, "preview namespace deleted", func() bool {
			out, err := kubectl("get", "namespace", previewNS, "--ignore-not-found", "-o", "jsonpath={.status.phase}")
			return err == nil && (out == "" || out == "Terminating")
		})
	})
}

// login runs the authorization code flow through the Keycloak login form.
func login(t *testing.T, c *http.Client, user string) {
	t.Helper()
	resp, err := c.Get(server + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	m := regexp.MustCompile(`<form[^>]*action="([^"]+)"`).FindSubmatch(page)
	if m == nil {
		t.Fatalf("no login form at %s:\n%s", resp.Request.URL, page)
	}
	resp, err = c.PostForm(html.UnescapeString(string(m[1])), url.Values{"username": {user}, "password": {"dev"}, "credentialId": {""}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	var me struct{ User struct{ Username string } }
	api(t, c, "GET", "/api/me", nil, &me, 200)
	if me.User.Username != user {
		t.Fatalf("logged in as %q, want %q", me.User.Username, user)
	}
}

func api(t *testing.T, c *http.Client, method, path string, body, out any, want int) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, server+path, rd)
	req.Header.Set("X-Requested-With", "simple-cd")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("%s %s: %s: %s", method, path, resp.Status, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
	}
}

// followLog reads the log stream until the deployment ends.
func followLog(t *testing.T, c *http.Client, id int64) (status, log string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/api/deployments/%d/log", server, id), nil)
	stream := &http.Client{Jar: c.Jar} // no client timeout, the context bounds it
	resp, err := stream.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	event := ""
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: ") && event == "status":
			status = strings.TrimPrefix(line, "data: ")
		case strings.HasPrefix(line, "data: ") && event == "log":
			b.WriteString(strings.TrimPrefix(line, "data: ") + "\n")
		}
	}
	return status, b.String()
}

type run struct {
	ID         int64
	SHA        string
	Image      string
	Event      string
	Branch     string
	PR         int
	AutoDeploy []string
}

// publish does what a workflow with cd-publish does: the run and its files go
// to the fake GitHub, then GitHub sends the workflow_run webhook.
func publish(t *testing.T, r run) {
	t.Helper()
	manifest, _ := json.Marshal(map[string]any{
		"version": 1, "images": []any{map[string]string{"name": "app", "ref": r.Image}},
		"helm": map[string]string{"version": "1.0.0"}, "autoDeploy": r.AutoDeploy,
	})
	var pulls []int
	if r.PR != 0 {
		pulls = []int{r.PR}
	}
	post(t, fakeGitHub+"/_e2e/runs", map[string]any{
		"repo": repo, "id": r.ID, "sha": r.SHA, "deployJson": string(manifest), "pulls": pulls,
		"files": map[string]string{".cd.yaml": repoConfig, "deploy/values-test.yaml": "message: from the repository\n"},
	})
	var prs []map[string]int
	for _, n := range pulls {
		prs = append(prs, map[string]int{"number": n})
	}
	webhook(t, "workflow_run", map[string]any{
		"action": "completed", "repository": map[string]any{"full_name": repo}, "installation": map[string]any{"id": 1},
		"workflow_run": map[string]any{
			"id": r.ID, "path": ".github/workflows/ci.yml", "event": r.Event, "status": "completed", "conclusion": "success",
			"head_branch": r.Branch, "head_sha": r.SHA, "html_url": "https://github.com/example/demo/actions/runs/" + fmt.Sprint(r.ID),
			"head_repository": map[string]any{"full_name": repo}, "triggering_actor": map[string]any{"login": "octocat"},
			"pull_requests": prs,
		},
	})
}

func webhook(t *testing.T, event string, payload any) {
	t.Helper()
	body, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(body)
	req, _ := http.NewRequest("POST", server+"/webhooks/github", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", fmt.Sprint(time.Now().UnixNano()))
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("webhook %s: %s", event, resp.Status)
	}
}

func post(t *testing.T, u string, body any) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(u, "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s: %s", u, resp.Status)
	}
}

type artifact struct {
	ID        int64
	SHA       string
	Readiness map[string][]string
}

func waitArtifact(t *testing.T, c *http.Client, sha string) artifact {
	t.Helper()
	var found artifact
	eventually(t, 30*time.Second, "artifact "+sha, func() bool {
		var list []artifact
		api(t, c, "GET", "/api/projects/"+repo+"/artifacts", nil, &list, 200)
		for _, a := range list {
			if a.SHA == sha {
				found = a
				return true
			}
		}
		return false
	})
	return found
}

// waitStatus waits until simple-cd reported state for the environment to GitHub.
func waitStatus(t *testing.T, env, ref, state string) {
	t.Helper()
	eventually(t, 6*time.Minute, fmt.Sprintf("GitHub status %s for %s", state, env), func() bool {
		resp, err := http.Get(fakeGitHub + "/_e2e/statuses")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		var list []struct{ Environment, Ref, State string }
		json.NewDecoder(resp.Body).Decode(&list)
		for _, s := range list {
			if s.Environment == env && s.Ref == ref && s.State == state {
				return true
			}
			if s.Environment == env && s.Ref == ref && (s.State == "failure" || s.State == "error") {
				t.Fatalf("deployment to %s failed, see the deployment log in simple-cd", env)
			}
		}
		return false
	})
}

// digest asks the registry for the digest of a tag.
func digest(t *testing.T, repository, tag string) string {
	t.Helper()
	req, _ := http.NewRequest("HEAD", registry+"/v2/"+repository+"/manifests/"+tag, nil)
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json",
	}, ", "))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("registry: %v (did you run make e2e-up?)", err)
	}
	resp.Body.Close()
	d := resp.Header.Get("Docker-Content-Digest")
	if resp.StatusCode != 200 || d == "" {
		t.Fatalf("registry %s:%s: %s", repository, tag, resp.Status)
	}
	return d
}

func kubectl(args ...string) (string, error) {
	out, err := exec.Command("kubectl", append([]string{"--context", kubeContext}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func kubectlJSON[T any](t *testing.T, args ...string) T {
	t.Helper()
	var v T
	out, err := kubectl(append(args, "-o", "json")...)
	if err != nil {
		t.Fatalf("kubectl %v: %v\n%s", args, err, out)
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func configMap(t *testing.T, ns string) map[string]string {
	t.Helper()
	return kubectlJSON[struct{ Data map[string]string }](t, "-n", ns, "get", "configmap", "demo").Data
}

func eventually(t *testing.T, timeout time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
		}
		time.Sleep(time.Second)
	}
}
