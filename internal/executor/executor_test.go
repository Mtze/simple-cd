package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var fakeTools = []string{"helm", "kubectl", "ssh", "git", "ansible-playbook"}

// The test binary doubles as every external tool: setup symlinks it under the
// tool names, and TestMain records each call to $FAKE_DIR/calls.jsonl.
func TestMain(m *testing.M) {
	if name := filepath.Base(os.Args[0]); slices.Contains(fakeTools, name) {
		os.Exit(fakeTool(name, os.Args[1:]))
	}
	os.Exit(m.Run())
}

type call struct {
	Name  string            `json:"name"`
	Args  []string          `json:"args"`
	Stdin string            `json:"stdin"`
	Dir   string            `json:"dir"`
	Env   map[string]string `json:"env"`
}

func fakeTool(name string, args []string) int {
	dir := os.Getenv("FAKE_DIR")
	stdin, _ := io.ReadAll(os.Stdin)
	wd, _ := os.Getwd()
	env := map[string]string{}
	for _, k := range []string{"KUBECONFIG", "HELM_REGISTRY_CONFIG", "GIT_SSH_COMMAND", "ANSIBLE_SSH_ARGS", "ANSIBLE_HOST_KEY_CHECKING"} {
		if v, ok := os.LookupEnv(k); ok {
			env[k] = v
		}
	}
	line, _ := json.Marshal(call{name, args, string(stdin), wd, env})
	f, err := os.OpenFile(filepath.Join(dir, "calls.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 99
	}
	f.Write(append(line, '\n'))
	f.Close()

	switch {
	case name == "kubectl" && len(args) > 1 && args[0] == "get":
		if ns, err := os.ReadFile(filepath.Join(dir, "namespace.json")); err == nil {
			os.Stdout.Write(ns)
		}
	case name == "git" && len(args) > 0 && args[0] == "clone":
		os.MkdirAll(args[len(args)-1], 0o700)
	case name == "git" && len(args) > 0 && args[0] == "checkout":
		if _, err := os.Stat(filepath.Join(dir, "missing-ref-"+args[len(args)-1])); err == nil {
			return 1
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "fail-"+name)); err == nil {
		os.Stderr.WriteString(name + " failed\n")
		return 1
	}
	return 0
}

type env struct {
	fake, secrets string
	vars          map[string]string
	vaultNS       []string // X-Vault-Namespace headers seen
	out           strings.Builder
}

func (e *env) getenv(k string) string { return e.vars[k] }

func (e *env) run(t *testing.T, spec Spec) error {
	t.Helper()
	return Run(context.Background(), spec, e.getenv, &e.out)
}

func (e *env) calls(t *testing.T) []call {
	t.Helper()
	f, err := os.Open(filepath.Join(e.fake, "calls.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []call
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var c call
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// find returns the calls of tool whose args start with prefix.
func (e *env) find(t *testing.T, tool string, prefix ...string) []call {
	var out []call
	for _, c := range e.calls(t) {
		if c.Name == tool && len(c.Args) >= len(prefix) && slices.Equal(c.Args[:len(prefix)], prefix) {
			out = append(out, c)
		}
	}
	return out
}

// assertSafe checks secret file modes and that no secret value reached stdout.
func (e *env) assertSafe(t *testing.T, secrets map[string]map[string]any) {
	t.Helper()
	entries, _ := os.ReadDir(e.secrets)
	for _, de := range entries {
		info, err := de.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %v, want 0600", de.Name(), info.Mode().Perm())
		}
	}
	for _, data := range secrets {
		for k, v := range data {
			if k == "user" || k == "username" {
				continue
			}
			for _, line := range strings.Split(v.(string), "\n") {
				if len(line) > 4 && strings.Contains(e.out.String(), line) {
					t.Errorf("stdout contains secret %s: %q", k, line)
				}
			}
		}
	}
}

const saJWT = "sa-jwt-token"

func setup(t *testing.T, secrets map[string]map[string]any) *env {
	t.Helper()
	e := &env{fake: t.TempDir(), secrets: filepath.Join(t.TempDir(), "secrets")}

	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range fakeTools {
		if err := os.Symlink(self, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("FAKE_DIR", e.fake)
	t.Setenv("TMPDIR", t.TempDir())

	const token = "vault-client-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.vaultNS = append(e.vaultNS, r.Header.Get("X-Vault-Namespace"))
		if r.Method == http.MethodPost && r.URL.Path == "/v1/auth/kubernetes/login" {
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			if req["role"] != "cd-executor" || req["jwt"] != saJWT {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"errors":["permission denied"]}`))
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"auth": map[string]any{"client_token": token}})
			return
		}
		if r.Header.Get("X-Vault-Token") != token {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"errors":["permission denied"]}`))
			return
		}
		ref, ok := strings.CutPrefix(r.URL.Path, "/v1/secret/data/")
		data, found := secrets["secret/"+ref]
		if r.Method != http.MethodGet || !ok || !found {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errors":[]}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data, "metadata": map[string]any{"version": 1}}})
	}))
	t.Cleanup(srv.Close)

	jwtFile := filepath.Join(t.TempDir(), "token")
	os.WriteFile(jwtFile, []byte(saJWT+"\n"), 0o600)
	e.vars = map[string]string{
		"VAULT_ADDR":             srv.URL,
		"SIMPLECD_VAULT_ROLE":    "cd-executor",
		"SIMPLECD_SA_TOKEN_FILE": jwtFile,
		"SIMPLECD_SECRETS_DIR":   e.secrets,
	}
	return e
}

var clusterSecret = map[string]any{"kubeconfig": "apiVersion: v1\nkind: Config\nclusters: [kube-secret-cluster]\n"}

func helmSpec(h HelmSpec) Spec {
	h.ClusterSecret = "secret/cd/clusters/test"
	if h.Release == "" {
		h.Release = "app"
	}
	return Spec{DeploymentID: 7, Kind: "deploy", Type: "helm", TargetID: "test", Repo: "org/app", SHA: "abc", Helm: &h}
}

func TestHelmDeploy(t *testing.T) {
	secrets := map[string]map[string]any{
		"secret/cd/clusters/test": clusterSecret,
		"secret/cd/values/test":   {"values": "replicas: 3\ndb:\n  password: s3cret-db-password\n"},
		"secret/cd/registry":      {"username": "robot", "password": "registry-pass-123"},
	}
	e := setup(t, secrets)
	spec := helmSpec(HelmSpec{
		Namespace: "app-test", Chart: "oci://ghcr.io/org/charts/app", Version: "1.2.3",
		Values:         map[string]any{"replicas": 1, "image": map[string]any{"repository": "ghcr.io/org/app", "tag": "old"}},
		ValuesSecret:   "secret/cd/values/test",
		Set:            map[string]any{"image": map[string]any{"tag": "new"}, "replicas": 5},
		RegistrySecret: "secret/cd/registry",
	})
	if err := e.run(t, spec); err != nil {
		t.Fatalf("run: %v\n%s", err, e.out.String())
	}

	login := e.find(t, "helm", "registry", "login")
	if len(login) != 1 {
		t.Fatalf("want one registry login, got %v", login)
	}
	if want := []string{"registry", "login", "ghcr.io", "--username", "robot", "--password-stdin"}; !slices.Equal(login[0].Args, want) {
		t.Errorf("login args %v, want %v", login[0].Args, want)
	}
	if login[0].Stdin != "registry-pass-123" {
		t.Errorf("password not passed on stdin")
	}
	if !strings.HasPrefix(login[0].Env["HELM_REGISTRY_CONFIG"], e.secrets) {
		t.Errorf("registry config outside secrets dir: %q", login[0].Env["HELM_REGISTRY_CONFIG"])
	}

	up := e.find(t, "helm", "upgrade")
	if len(up) != 1 {
		t.Fatalf("want one helm upgrade, got %v", up)
	}
	valuesFile := filepath.Join(e.secrets, "values.yaml")
	want := []string{"upgrade", "--install", "app", "oci://ghcr.io/org/charts/app", "--version", "1.2.3",
		"--namespace", "app-test", "--values", valuesFile, "--wait", "--timeout", "10m"}
	if !slices.Equal(up[0].Args, want) {
		t.Errorf("helm args\n got %v\nwant %v", up[0].Args, want)
	}
	if slices.Contains(up[0].Args, "--create-namespace") {
		t.Error("must not use --create-namespace")
	}
	if kc := up[0].Env["KUBECONFIG"]; kc != filepath.Join(e.secrets, "kubeconfig") {
		t.Errorf("KUBECONFIG = %q", kc)
	}
	if len(e.find(t, "kubectl")) != 0 {
		t.Error("non-preview deploy must not touch namespaces")
	}

	raw, err := os.ReadFile(valuesFile)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	wantValues := map[string]any{
		"replicas": 5, // spec.Set beats Vault beats spec.Values
		"image":    map[string]any{"repository": "ghcr.io/org/app", "tag": "new"},
		"db":       map[string]any{"password": "s3cret-db-password"},
	}
	if !reflect.DeepEqual(got, wantValues) {
		t.Errorf("values\n got %v\nwant %v", got, wantValues)
	}
	e.assertSafe(t, secrets)
}

func TestHelmValuesVaultOverridesRepo(t *testing.T) {
	secrets := map[string]map[string]any{
		"secret/cd/clusters/test": clusterSecret,
		"secret/cd/values/test":   {"values": "replicas: 3\n"},
	}
	e := setup(t, secrets)
	spec := helmSpec(HelmSpec{Namespace: "ns", Chart: "oci://r.example/c", Version: "1", Timeout: "5m0s",
		Values: map[string]any{"replicas": 1, "keep": true}, ValuesSecret: "secret/cd/values/test"})
	if err := e.run(t, spec); err != nil {
		t.Fatalf("run: %v\n%s", err, e.out.String())
	}
	raw, _ := os.ReadFile(filepath.Join(e.secrets, "values.yaml"))
	var got map[string]any
	yaml.Unmarshal(raw, &got)
	if !reflect.DeepEqual(got, map[string]any{"replicas": 3, "keep": true}) {
		t.Errorf("values = %v", got)
	}
	if up := e.find(t, "helm", "upgrade"); len(up) != 1 || !slices.Contains(up[0].Args, "5m0s") {
		t.Errorf("timeout from spec not used: %v", up)
	}
	if len(e.find(t, "helm", "registry")) != 0 {
		t.Error("no registry login without registry secret")
	}
}

func TestMissingVaultKey(t *testing.T) {
	e := setup(t, map[string]map[string]any{"secret/cd/clusters/test": {"other": "x"}})
	err := e.run(t, helmSpec(HelmSpec{Namespace: "ns", Chart: "oci://r.example/c", Version: "1"}))
	if err == nil || !strings.Contains(err.Error(), `no key "kubeconfig"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestVaultLoginDenied(t *testing.T) {
	e := setup(t, nil)
	e.vars["SIMPLECD_VAULT_ROLE"] = "wrong"
	err := e.run(t, helmSpec(HelmSpec{Namespace: "ns", Chart: "oci://r.example/c", Version: "1"}))
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v", err)
	}
}

var previewLabels = map[string]string{"cd/managed": "true", "cd/target": "preview", "cd/pr": "42"}

func writeNamespace(t *testing.T, e *env, labels map[string]string) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"kind": "Namespace", "metadata": map[string]any{"name": "pr-app-42", "labels": labels}})
	if err := os.WriteFile(filepath.Join(e.fake, "namespace.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewDeploy(t *testing.T) {
	foreign := map[string]string{"team": "x", "cd/managed": "true"}
	tests := []struct {
		name       string
		existing   map[string]string // nil: namespace does not exist
		wantErr    bool
		wantCreate bool
	}{
		{name: "missing namespace is created", wantCreate: true},
		{name: "own namespace is reused", existing: map[string]string{"cd/managed": "true", "cd/target": "preview", "cd/pr": "42", "extra": "ok"}},
		{name: "foreign namespace is refused", existing: foreign, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setup(t, map[string]map[string]any{"secret/cd/clusters/test": clusterSecret})
			if tt.existing != nil {
				writeNamespace(t, e, tt.existing)
			}
			spec := helmSpec(HelmSpec{Namespace: "pr-app-42", Chart: "oci://r.example/c", Version: "1", Preview: true, Labels: previewLabels})
			err := e.run(t, spec)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "namespace pr-app-42 exists and is not managed by simple-cd") {
					t.Fatalf("err = %v", err)
				}
				if len(e.find(t, "helm")) != 0 || len(e.find(t, "kubectl", "create")) != 0 {
					t.Error("must not create or install into a foreign namespace")
				}
				return
			}
			if err != nil {
				t.Fatalf("run: %v\n%s", err, e.out.String())
			}
			create := e.find(t, "kubectl", "create", "-f", "-")
			if !tt.wantCreate {
				if len(create) != 0 {
					t.Error("existing namespace must not be created again")
				}
			} else {
				if len(create) != 1 {
					t.Fatalf("want one kubectl create, got %v", create)
				}
				var ns struct {
					Kind     string
					Metadata struct {
						Name   string
						Labels map[string]string
					}
				}
				if err := json.Unmarshal([]byte(create[0].Stdin), &ns); err != nil {
					t.Fatal(err)
				}
				if ns.Kind != "Namespace" || ns.Metadata.Name != "pr-app-42" || !reflect.DeepEqual(ns.Metadata.Labels, previewLabels) {
					t.Errorf("namespace manifest = %+v", ns)
				}
			}
			if len(e.find(t, "helm", "upgrade")) != 1 {
				t.Error("helm upgrade not called")
			}
		})
	}
}

func TestTeardown(t *testing.T) {
	tests := []struct {
		name       string
		existing   map[string]string
		wantErr    bool
		wantDelete bool
	}{
		{name: "missing namespace is a no-op"},
		{name: "matching labels deletes release and namespace", existing: previewLabels, wantDelete: true},
		{name: "mismatched labels deletes nothing", existing: map[string]string{"cd/managed": "true", "cd/target": "preview", "cd/pr": "43"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setup(t, map[string]map[string]any{"secret/cd/clusters/test": clusterSecret})
			if tt.existing != nil {
				writeNamespace(t, e, tt.existing)
			}
			spec := Spec{Kind: "teardown", Type: "helm", TargetID: "preview", Helm: &HelmSpec{
				ClusterSecret: "secret/cd/clusters/test", Namespace: "pr-app-42", Release: "app", Preview: true, Labels: previewLabels,
			}}
			err := e.run(t, spec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v\n%s", err, e.out.String())
			}
			uninstall := e.find(t, "helm", "uninstall")
			del := e.find(t, "kubectl", "delete")
			if !tt.wantDelete {
				if len(uninstall)+len(del) != 0 {
					t.Errorf("deleted something: %v %v", uninstall, del)
				}
				return
			}
			if len(uninstall) != 1 || !slices.Equal(uninstall[0].Args, []string{"uninstall", "app", "--namespace", "pr-app-42", "--ignore-not-found"}) {
				t.Errorf("uninstall = %v", uninstall)
			}
			if len(del) != 1 || !slices.Equal(del[0].Args, []string{"delete", "namespace", "pr-app-42", "--wait=false"}) {
				t.Errorf("delete = %v", del)
			}
		})
	}
}

func TestTeardownNeedsLabels(t *testing.T) {
	e := setup(t, map[string]map[string]any{"secret/cd/clusters/test": clusterSecret})
	writeNamespace(t, e, map[string]string{"anything": "x"})
	spec := Spec{Kind: "teardown", Type: "helm", Helm: &HelmSpec{ClusterSecret: "secret/cd/clusters/test", Namespace: "pr-app-42", Release: "app", Preview: true}}
	if err := e.run(t, spec); err == nil {
		t.Fatal("teardown without labels must fail, it would match any namespace")
	}
	if len(e.calls(t)) != 0 {
		t.Error("nothing may be called")
	}
}

var sshSecret = map[string]any{
	"private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\nsecret-key-material\n-----END OPENSSH PRIVATE KEY-----",
	"known_hosts": "vm.example.org ssh-ed25519 AAAAhostkey",
	"user":        "ops",
}

func composeSpec() Spec {
	return Spec{Kind: "deploy", Type: "compose", TargetID: "vm", Compose: &ComposeSpec{
		Host: "vm.example.org", Dir: "/opt/app", SSHSecret: "secret/cd/ssh/vm",
		ComposeFile: "services:\n  web:\n    image: ${WEB_IMAGE}\n",
		Env:         map[string]string{"WEB_IMAGE": "ghcr.io/org/web@sha256:aa", "API_IMAGE": "ghcr.io/org/api@sha256:bb"},
	}}
}

func TestCompose(t *testing.T) {
	secrets := map[string]map[string]any{"secret/cd/ssh/vm": sshSecret}
	e := setup(t, secrets)
	// Local dev path: a static token instead of Kubernetes auth, plus a namespace.
	delete(e.vars, "SIMPLECD_VAULT_ROLE")
	e.vars["VAULT_TOKEN"] = "vault-client-token"
	e.vars["VAULT_NAMESPACE"] = "team-a"

	if err := e.run(t, composeSpec()); err != nil {
		t.Fatalf("run: %v\n%s", err, e.out.String())
	}
	if len(e.vaultNS) == 0 || e.vaultNS[0] != "team-a" {
		t.Errorf("vault namespace header = %v", e.vaultNS)
	}
	calls := e.find(t, "ssh")
	if len(calls) != 3 {
		t.Fatalf("want 3 ssh calls, got %d: %v", len(calls), calls)
	}
	key, kh := filepath.Join(e.secrets, "ssh_key"), filepath.Join(e.secrets, "known_hosts")
	wantOpts := []string{"-i", key, "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + kh, "ops@vm.example.org"}
	for _, c := range calls {
		if !slices.Equal(c.Args[:len(wantOpts)], wantOpts) {
			t.Errorf("ssh args %v", c.Args)
		}
	}
	remote := func(i int) string { return calls[i].Args[len(calls[i].Args)-1] }
	if !strings.Contains(remote(0), "mkdir -p '/opt/app'") || !strings.HasSuffix(remote(0), "'/opt/app'/docker-compose.yml") {
		t.Errorf("compose upload command %q", remote(0))
	}
	if calls[0].Stdin != composeSpec().Compose.ComposeFile {
		t.Errorf("compose file content %q", calls[0].Stdin)
	}
	if !strings.HasSuffix(remote(1), "'/opt/app'/.cd.env") {
		t.Errorf(".cd.env upload command %q", remote(1))
	}
	if want := "API_IMAGE=ghcr.io/org/api@sha256:bb\nWEB_IMAGE=ghcr.io/org/web@sha256:aa\n"; calls[1].Stdin != want {
		t.Errorf(".cd.env = %q, want %q", calls[1].Stdin, want)
	}
	for _, part := range []string{"cd '/opt/app'", "if [ -f .env ]", "--env-file .cd.env pull", "--env-file .cd.env up -d --remove-orphans"} {
		if !strings.Contains(remote(2), part) {
			t.Errorf("remote command %q lacks %q", remote(2), part)
		}
	}
	keyData, _ := os.ReadFile(key)
	if !strings.HasSuffix(string(keyData), "-----\n") {
		t.Error("private key must end with a newline")
	}
	e.assertSafe(t, secrets)
}

func TestComposeRejects(t *testing.T) {
	noKH := map[string]any{"private_key": sshSecret["private_key"]}
	tests := []struct {
		name   string
		secret map[string]any
		mutate func(*ComposeSpec)
		want   string
	}{
		{"missing known_hosts", noKH, func(*ComposeSpec) {}, `no key "known_hosts"`},
		{"newline in env value", sshSecret, func(c *ComposeSpec) { c.Env["X"] = "a\nEVIL=1" }, "contains a newline"},
		{"relative dir", sshSecret, func(c *ComposeSpec) { c.Dir = "opt/app" }, "compose dir"},
		{"dir with quote", sshSecret, func(c *ComposeSpec) { c.Dir = "/opt/a'pp" }, "compose dir"},
		{"host starting with dash", sshSecret, func(c *ComposeSpec) { c.Host = "-oProxyCommand=x" }, "compose host"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setup(t, map[string]map[string]any{"secret/cd/ssh/vm": tt.secret})
			spec := composeSpec()
			tt.mutate(spec.Compose)
			err := e.run(t, spec)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if len(e.find(t, "ssh")) != 0 {
				t.Error("ssh must not be called")
			}
		})
	}
}

const sha = "0123456789abcdef0123456789abcdef01234567"

func ansibleSpec() Spec {
	return Spec{Kind: "deploy", Type: "ansible", TargetID: "prod", SHA: sha, Ansible: &AnsibleSpec{
		RepoURL: "git@github.com:org/infra.git", Ref: sha, Playbook: "deploy/site.yml", Inventory: "inventories/prod",
		CredentialsSecret: "secret/cd/ansible/prod",
		ExtraVars:         map[string]any{"cd_sha": sha, "cd_images": map[string]any{"web": "ghcr.io/org/web@sha256:aa"}},
	}}
}

func TestAnsible(t *testing.T) {
	secrets := map[string]map[string]any{"secret/cd/ansible/prod": sshSecret}
	e := setup(t, secrets)
	if err := e.run(t, ansibleSpec()); err != nil {
		t.Fatalf("run: %v\n%s", err, e.out.String())
	}
	key, kh := filepath.Join(e.secrets, "ssh_key"), filepath.Join(e.secrets, "known_hosts")

	clone := e.find(t, "git", "clone")
	if len(clone) != 1 || clone[0].Args[3] != "git@github.com:org/infra.git" {
		t.Fatalf("clone = %v", clone)
	}
	checkoutDir := clone[0].Args[4]
	gitSSH := clone[0].Env["GIT_SSH_COMMAND"]
	for _, part := range []string{"-i '" + key + "'", "StrictHostKeyChecking=yes", "UserKnownHostsFile='" + kh + "'"} {
		if !strings.Contains(gitSSH, part) {
			t.Errorf("GIT_SSH_COMMAND %q lacks %q", gitSSH, part)
		}
	}
	co := e.find(t, "git", "checkout")
	if len(co) != 1 || !slices.Equal(co[0].Args, []string{"checkout", "--quiet", "--detach", sha}) || co[0].Dir != realPath(t, checkoutDir) {
		t.Errorf("checkout = %v", co)
	}

	pb := e.find(t, "ansible-playbook")
	if len(pb) != 1 {
		t.Fatalf("ansible-playbook calls = %v", pb)
	}
	vars := filepath.Join(e.secrets, "extra-vars.json")
	want := []string{"-i", "inventories/prod", "deploy/site.yml", "--private-key", key, "-e", "@" + vars, "-u", "ops"}
	if !slices.Equal(pb[0].Args, want) {
		t.Errorf("ansible args\n got %v\nwant %v", pb[0].Args, want)
	}
	if pb[0].Dir != realPath(t, checkoutDir) {
		t.Errorf("ansible ran in %q, want %q", pb[0].Dir, checkoutDir)
	}
	if pb[0].Env["ANSIBLE_HOST_KEY_CHECKING"] != "True" || !strings.Contains(pb[0].Env["ANSIBLE_SSH_ARGS"], "-o UserKnownHostsFile="+kh) {
		t.Errorf("ansible env = %v", pb[0].Env)
	}
	raw, _ := os.ReadFile(vars)
	var got map[string]any
	json.Unmarshal(raw, &got)
	if got["cd_sha"] != sha || got["cd_images"].(map[string]any)["web"] != "ghcr.io/org/web@sha256:aa" {
		t.Errorf("extra vars = %s", raw)
	}
	if _, err := os.Stat(checkoutDir); !os.IsNotExist(err) {
		t.Error("checkout dir not cleaned up")
	}
	e.assertSafe(t, secrets)
}

func TestAnsibleBranchFallsBackToOrigin(t *testing.T) {
	e := setup(t, map[string]map[string]any{"secret/cd/ansible/prod": sshSecret})
	os.WriteFile(filepath.Join(e.fake, "missing-ref-release"), nil, 0o600)
	spec := ansibleSpec()
	spec.Ansible.Ref = "release"
	if err := e.run(t, spec); err != nil {
		t.Fatalf("run: %v\n%s", err, e.out.String())
	}
	co := e.find(t, "git", "checkout")
	if len(co) != 2 || co[1].Args[len(co[1].Args)-1] != "origin/release" {
		t.Errorf("checkout = %v", co)
	}
}

func TestAnsibleRejects(t *testing.T) {
	noKH := map[string]any{"private_key": "k"}
	tests := []struct {
		name   string
		secret map[string]any
		mutate func(*AnsibleSpec)
		want   string
	}{
		{"playbook traversal", sshSecret, func(a *AnsibleSpec) { a.Playbook = "../../etc/site.yml" }, "inside the repository"},
		{"absolute inventory", sshSecret, func(a *AnsibleSpec) { a.Inventory = "/etc/ansible/hosts" }, "inside the repository"},
		{"ssh url without known_hosts", noKH, func(*AnsibleSpec) {}, "needs known_hosts"},
		{"file url", sshSecret, func(a *AnsibleSpec) { a.RepoURL = "file:///etc" }, "only https://"},
		{"ref option", sshSecret, func(a *AnsibleSpec) { a.Ref = "--orphan" }, "ref"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setup(t, map[string]map[string]any{"secret/cd/ansible/prod": tt.secret})
			spec := ansibleSpec()
			tt.mutate(spec.Ansible)
			err := e.run(t, spec)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if len(e.calls(t)) != 0 {
				t.Error("no tool may run")
			}
		})
	}
}

func TestAnsibleHTTPSWithoutKnownHosts(t *testing.T) {
	e := setup(t, map[string]map[string]any{"secret/cd/ansible/prod": {"private_key": "k"}})
	spec := ansibleSpec()
	spec.Ansible.RepoURL = "https://github.com/org/infra.git"
	if err := e.run(t, spec); err != nil {
		t.Fatalf("run: %v\n%s", err, e.out.String())
	}
	pb := e.find(t, "ansible-playbook")
	if len(pb) != 1 || slices.Contains(pb[0].Args, "-u") || strings.Contains(pb[0].Env["ANSIBLE_SSH_ARGS"], "UserKnownHostsFile") {
		t.Errorf("ansible-playbook = %v", pb)
	}
}

func TestUnsupportedSpec(t *testing.T) {
	e := setup(t, nil)
	if err := e.run(t, Spec{Kind: "teardown", Type: "compose", Compose: &ComposeSpec{}}); err == nil {
		t.Fatal("want error")
	}
}

// realPath resolves symlinks such as macOS /var -> /private/var, since the
// fake tools see the resolved working directory.
func realPath(t *testing.T, p string) string {
	t.Helper()
	dir, base := filepath.Split(p)
	r, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(r, base)
}
