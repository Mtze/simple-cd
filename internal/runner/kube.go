package runner

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Mtze/simple-cd/internal/config"
)

const saDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// maxSpecBytes stays below the kernel's 128 KiB limit for one environment variable.
const maxSpecBytes = 120 << 10

// Kube runs executors as Kubernetes Jobs in the server's own namespace,
// talking to the API server over plain REST (no client-go).
type Kube struct {
	Host           string // https://host:port
	Namespace      string
	TokenFile      string // re-read on every request, tokens rotate
	HTTP           *http.Client
	Image          string
	AnsibleImage   string
	ServiceAccount string
	Env            map[string]string // passed to the executor, e.g. VAULT_ADDR
}

// InCluster configures the runner from the pod's service account.
func InCluster() (*Kube, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" {
		return nil, errors.New("not running in Kubernetes (KUBERNETES_SERVICE_HOST unset)")
	}
	ca, err := os.ReadFile(saDir + "/ca.crt")
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	ns, err := os.ReadFile(saDir + "/namespace")
	if err != nil {
		return nil, err
	}
	return &Kube{
		Host:      "https://" + host + ":" + port,
		Namespace: strings.TrimSpace(string(ns)),
		TokenFile: saDir + "/token",
		HTTP:      &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}},
	}, nil
}

func (k *Kube) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.Host+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if k.TokenFile != "" {
		tok, err := os.ReadFile(k.TokenFile)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
	}
	return k.HTTP.Do(req)
}

func (k *Kube) call(ctx context.Context, method, path string, body, out any) error {
	resp, err := k.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode == http.StatusConflict {
		return ErrExists
	}
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("kubernetes %s %s: %s: %s", method, path, resp.Status, msg)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func (k *Kube) jobsPath() string { return "/apis/batch/v1/namespaces/" + k.Namespace + "/jobs" }

// Manifest builds the Job. Exported for tests and docs.
func (k *Kube) Manifest(j Job) (map[string]any, error) {
	spec, err := json.Marshal(j.Spec)
	if err != nil {
		return nil, err
	}
	if len(spec) > maxSpecBytes {
		// shortcut: the spec travels as an env var; move it into a ConfigMap if values files ever get this big.
		return nil, fmt.Errorf("deployment spec is %d KiB, the limit is %d KiB", len(spec)>>10, maxSpecBytes>>10)
	}
	image := k.Image
	if j.Spec.Type == config.TypeAnsible {
		image = k.AnsibleImage
	}
	env := []map[string]any{
		{"name": "SIMPLECD_SPEC", "value": string(spec)},
		{"name": "SIMPLECD_SECRETS_DIR", "value": "/secrets"},
		{"name": "HOME", "value": "/tmp"},
	}
	keys := make([]string, 0, len(k.Env))
	for key := range k.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, map[string]any{"name": key, "value": k.Env[key]})
	}
	labels := map[string]any{
		"app.kubernetes.io/name": "simple-cd-executor", "app.kubernetes.io/managed-by": "simple-cd",
		"simple-cd/deployment": fmt.Sprint(j.Spec.DeploymentID),
	}
	return map[string]any{
		"apiVersion": "batch/v1", "kind": "Job",
		"metadata": map[string]any{"name": j.Name, "labels": labels},
		"spec": map[string]any{
			"backoffLimit":            0,
			"activeDeadlineSeconds":   int64(j.Timeout.Seconds()),
			"ttlSecondsAfterFinished": 3600,
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec": map[string]any{
					"restartPolicy":                "Never",
					"serviceAccountName":           k.ServiceAccount,
					"automountServiceAccountToken": true, // needed for the Vault login
					"securityContext": map[string]any{
						"runAsNonRoot": true, "runAsUser": 10001, "runAsGroup": 10001, "fsGroup": 10001,
						"seccompProfile": map[string]any{"type": "RuntimeDefault"},
					},
					"containers": []any{map[string]any{
						"name": "executor", "image": image, "env": env,
						"securityContext": map[string]any{
							"allowPrivilegeEscalation": false, "readOnlyRootFilesystem": true,
							"capabilities": map[string]any{"drop": []string{"ALL"}},
						},
						"resources": map[string]any{
							"requests": map[string]any{"cpu": "100m", "memory": "128Mi"},
							"limits":   map[string]any{"memory": "512Mi"},
						},
						"volumeMounts": []any{
							map[string]any{"name": "secrets", "mountPath": "/secrets"},
							map[string]any{"name": "tmp", "mountPath": "/tmp"},
						},
					}},
					"volumes": []any{
						map[string]any{"name": "secrets", "emptyDir": map[string]any{"medium": "Memory", "sizeLimit": "16Mi"}},
						map[string]any{"name": "tmp", "emptyDir": map[string]any{"sizeLimit": "1Gi"}},
					},
				},
			},
		},
	}, nil
}

func (k *Kube) Start(ctx context.Context, j Job) error {
	m, err := k.Manifest(j)
	if err != nil {
		return err
	}
	return k.call(ctx, http.MethodPost, k.jobsPath(), m, nil)
}

func (k *Kube) State(ctx context.Context, name string) (State, error) {
	var job struct {
		Status struct {
			Succeeded  int `json:"succeeded"`
			Failed     int `json:"failed"`
			Conditions []struct {
				Type    string `json:"type"`
				Status  string `json:"status"`
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"conditions"`
		} `json:"status"`
	}
	if err := k.call(ctx, http.MethodGet, k.jobsPath()+"/"+name, nil, &job); err != nil {
		return State{}, err
	}
	s := job.Status
	if s.Succeeded > 0 {
		return State{Done: true, Succeeded: true}, nil
	}
	for _, c := range s.Conditions {
		if c.Type == "Failed" && c.Status == "True" {
			return State{Done: true, Message: strings.TrimSpace(c.Reason + ": " + c.Message)}, nil
		}
	}
	if s.Failed > 0 {
		return State{Done: true, Message: "executor failed"}, nil
	}
	return State{}, nil
}

func (k *Kube) pod(ctx context.Context, job string) (string, string, error) {
	var pods struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	path := "/api/v1/namespaces/" + k.Namespace + "/pods?labelSelector=" + url.QueryEscape("job-name="+job)
	if err := k.call(ctx, http.MethodGet, path, nil, &pods); err != nil {
		return "", "", err
	}
	if len(pods.Items) == 0 {
		return "", "", ErrNotFound
	}
	p := pods.Items[0]
	return p.Metadata.Name, p.Status.Phase, nil
}

// Logs waits for the pod to start when following, so the UI can attach early.
func (k *Kube) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) {
	for {
		pod, phase, err := k.pod(ctx, name)
		if err == nil && phase != "Pending" {
			path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log?container=executor&follow=%t", k.Namespace, pod, follow)
			resp, err := k.request(ctx, http.MethodGet, path, nil)
			if err != nil {
				return nil, err
			}
			if resp.StatusCode >= 300 {
				resp.Body.Close()
				return nil, fmt.Errorf("pod logs: %s", resp.Status)
			}
			return resp.Body, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if !follow {
			return io.NopCloser(strings.NewReader("")), nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (k *Kube) Delete(ctx context.Context, name string) error {
	err := k.call(ctx, http.MethodDelete, k.jobsPath()+"/"+name+"?propagationPolicy=Background", nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
