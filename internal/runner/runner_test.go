package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Mtze/simple-cd/internal/executor"
)

var ctx = context.Background()

func TestKubeRunner(t *testing.T) {
	var created map[string]any
	status := `{"status":{}}`
	mux := http.NewServeMux()
	mux.HandleFunc("POST /apis/batch/v1/namespaces/cd/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauthorized", 401)
			return
		}
		json.NewDecoder(r.Body).Decode(&created)
		w.WriteHeader(201)
		w.Write([]byte(`{}`))
	})
	mux.HandleFunc("GET /apis/batch/v1/namespaces/cd/jobs/simple-cd-7", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(status))
	})
	mux.HandleFunc("/apis/batch/v1/namespaces/cd/jobs/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /api/v1/namespaces/cd/pods", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("labelSelector") != "job-name=simple-cd-7" {
			http.Error(w, "selector", 400)
			return
		}
		w.Write([]byte(`{"items":[{"metadata":{"name":"pod-1"},"status":{"phase":"Running"}}]}`))
	})
	mux.HandleFunc("GET /api/v1/namespaces/cd/pods/pod-1/log", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("==> hello\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	tokenFile := filepath.Join(t.TempDir(), "token")
	os.WriteFile(tokenFile, []byte("tok\n"), 0o600)

	k := &Kube{Host: srv.URL, Namespace: "cd", TokenFile: tokenFile, HTTP: srv.Client(), Image: "exec:1", AnsibleImage: "exec-ansible:1",
		ServiceAccount: "simple-cd-executor", Env: map[string]string{"VAULT_ADDR": "https://vault"}}
	job := Job{Name: JobName(7), Spec: executor.Spec{DeploymentID: 7, Type: "helm"}, Timeout: 10 * time.Minute}
	if err := k.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	spec := created["spec"].(map[string]any)
	pod := spec["template"].(map[string]any)["spec"].(map[string]any)
	c := pod["containers"].([]any)[0].(map[string]any)
	sc := c["securityContext"].(map[string]any)
	if spec["activeDeadlineSeconds"].(float64) != 600 || spec["backoffLimit"].(float64) != 0 || c["image"] != "exec:1" ||
		sc["readOnlyRootFilesystem"] != true || pod["serviceAccountName"] != "simple-cd-executor" {
		t.Fatalf("manifest = %v", created)
	}
	envJSON, _ := json.Marshal(c["env"])
	if !strings.Contains(string(envJSON), `"VAULT_ADDR"`) || !strings.Contains(string(envJSON), `SIMPLECD_SPEC`) {
		t.Fatalf("env = %s", envJSON)
	}
	vols, _ := json.Marshal(pod["volumes"])
	if !strings.Contains(string(vols), `"medium":"Memory"`) {
		t.Fatalf("secrets volume not in memory: %s", vols)
	}

	if st, _ := k.State(ctx, "simple-cd-7"); st.Done {
		t.Fatal("running job reported done")
	}
	status = `{"status":{"failed":1,"conditions":[{"type":"Failed","status":"True","reason":"DeadlineExceeded","message":"too long"}]}}`
	if st, _ := k.State(ctx, "simple-cd-7"); !st.Done || st.Succeeded || !strings.HasPrefix(st.Message, "DeadlineExceeded") {
		t.Fatalf("state = %+v", st)
	}
	status = `{"status":{"succeeded":1}}`
	if st, _ := k.State(ctx, "simple-cd-7"); !st.Succeeded {
		t.Fatalf("state = %+v", st)
	}
	if _, err := k.State(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("missing job: %v", err)
	}
	rc, err := k.Logs(ctx, "simple-cd-7", false)
	if err != nil {
		t.Fatal(err)
	}
	logs, _ := io.ReadAll(rc)
	if string(logs) != "==> hello\n" {
		t.Fatalf("logs = %q", logs)
	}
	if err := k.Delete(ctx, "gone"); err != nil {
		t.Fatalf("deleting a missing job: %v", err)
	}

	job.Spec.Type = "ansible"
	m, _ := k.Manifest(job)
	img := m["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)["image"]
	if img != "exec-ansible:1" {
		t.Fatalf("ansible image = %v", img)
	}
	job.Spec.Helm = &executor.HelmSpec{Values: map[string]any{"big": strings.Repeat("x", maxSpecBytes)}}
	if _, err := k.Manifest(job); err == nil {
		t.Fatal("oversized spec accepted")
	}
}

func TestProcessRunner(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "executor.sh")
	os.WriteFile(bin, []byte("#!/bin/sh\necho start\nsleep 0.3\necho \"$SIMPLECD_SPEC\" | grep -q '\"deploymentId\":1' && echo ok && exit 0\nexit 3\n"), 0o700)
	p := &Process{Binary: bin, Dir: filepath.Join(dir, "logs")}

	if err := p.Start(ctx, Job{Name: "a", Spec: executor.Spec{DeploymentID: 1}, Timeout: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	rc, err := p.Logs(ctx, "a", true)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := io.ReadAll(rc)
	if string(out) != "start\nok\n" {
		t.Fatalf("followed logs = %q", out)
	}
	if st, _ := p.State(ctx, "a"); !st.Done || !st.Succeeded {
		t.Fatalf("state = %+v", st)
	}

	p.Start(ctx, Job{Name: "b", Spec: executor.Spec{DeploymentID: 2}, Timeout: 10 * time.Second})
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, _ := p.State(ctx, "b")
		if st.Done {
			if st.Succeeded {
				t.Fatal("failing executor reported success")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("process did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := p.State(ctx, "missing"); err != ErrNotFound {
		t.Fatal(err)
	}
}
