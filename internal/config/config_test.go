package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const valid = `
defaults:
  registries: [ghcr.io/org/]
  lockDuration: 2h
projects:
  org/app:
    registries: [ghcr.io/org/app]
targets:
  - id: app-test
    project: org/app
    type: helm
    lockable: true
    cluster: vault:secret/cd/clusters/test
    namespace: app
    values:
      replicas: 1
  - id: app-previews
    project: org/app
    type: helm
    preview: true
    previewTTL: 48h
    autoDeploy:
      from: [pr]
    cluster: vault:secret/cd/clusters/dev
  - id: app-prod
    project: org/app
    type: compose
    role: deploy:app-prod
    host: vm1.example.org
    ssh: vault:secret/cd/ssh/prod
    dir: /opt/app
    jobTimeout: 10m
  - id: tools-vm
    project: org/tools
    type: ansible
    ansible:
      source: git@github.com:org/infra.git
      ref: main
      playbook: playbooks/tools.yml
      inventory: inventories/prod
      credentials: vault:secret/cd/ssh/tools
`

func TestParseValid(t *testing.T) {
	c, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(c.Targets); got != 4 {
		t.Fatalf("targets = %d", got)
	}
	test, _ := c.Target("app-test")
	if test.Release != "app" {
		t.Errorf("default release = %q", test.Release)
	}
	if test.Values.Inline["replicas"] != 1 {
		t.Errorf("inline values = %v", test.Values.Inline)
	}
	if c.LockDuration(test) != 2*time.Hour {
		t.Errorf("lock duration = %v", c.LockDuration(test))
	}
	prev, _ := c.Target("app-previews")
	if c.PreviewTTL(prev) != 48*time.Hour {
		t.Errorf("preview ttl = %v", c.PreviewTTL(prev))
	}
	prod, _ := c.Target("app-prod")
	if c.JobTimeout(prod) != 10*time.Minute || c.JobTimeout(test) != 30*time.Minute {
		t.Errorf("job timeouts wrong")
	}
	if c.Defaults.AuditRetention.D() != 365*24*time.Hour || c.Defaults.KeepBranchArtifacts != 50 {
		t.Errorf("defaults not applied: %+v", c.Defaults)
	}
	if r := c.Registries("org/app"); r[0] != "ghcr.io/org/app" {
		t.Errorf("project registries = %v", r)
	}
	if r := c.Registries("org/tools"); r[0] != "ghcr.io/org/" {
		t.Errorf("default registries = %v", r)
	}
	if !c.HasProject("ORG/app") || c.HasProject("org/other") {
		t.Errorf("HasProject wrong")
	}
	if len(c.TargetsOf("org/app")) != 3 {
		t.Errorf("TargetsOf wrong")
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"unknown key": {`targets: [{id: a, project: o/r, type: helm, clustr: x}]`, "clustr"},
		"bad id":      {`defaults: {registries: [r/]}` + "\n" + `targets: [{id: A_B, project: o/r, type: compose, host: h, ssh: "vault:x", dir: /d}]`, "id must be"},
		"duplicate": {`defaults: {registries: [r/]}
targets:
  - {id: a, project: o/r, type: compose, host: h, ssh: "vault:x", dir: /d}
  - {id: a, project: o/r, type: compose, host: h, ssh: "vault:x", dir: /d}`, "duplicate"},
		"no registries":    {`targets: [{id: a, project: o/r, type: compose, host: h, ssh: "vault:x", dir: /d}]`, "no allowed registries"},
		"preview lockable": {`defaults: {registries: [r/]}` + "\n" + `targets: [{id: a, project: o/r, type: helm, cluster: "vault:c", preview: true, lockable: true}]`, "cannot be lockable"},
		"preview compose":  {`defaults: {registries: [r/]}` + "\n" + `targets: [{id: a, project: o/r, type: compose, host: h, ssh: "vault:x", dir: /d, preview: true}]`, "only supported for helm"},
		"secret inline":    {`defaults: {registries: [r/]}` + "\n" + `targets: [{id: a, project: o/r, type: compose, host: h, ssh: "-----BEGIN", dir: /d}]`, "vault: reference"},
		"relative dir":     {`defaults: {registries: [r/]}` + "\n" + `targets: [{id: a, project: o/r, type: compose, host: h, ssh: "vault:x", dir: opt}]`, "absolute"},
		"helm namespace":   {`defaults: {registries: [r/]}` + "\n" + `targets: [{id: a, project: o/r, type: helm, cluster: "vault:c"}]`, "namespace is required"},
		"bad source":       {`defaults: {registries: [r/]}` + "\n" + `targets: [{id: a, project: o/r, type: helm, cluster: "vault:c", namespace: n, autoDeploy: {from: [tag]}}]`, "unknown source"},
		"branches w/o branch": {`defaults: {registries: [r/]}` + "\n" + `targets: [{id: a, project: o/r, type: helm, cluster: "vault:c", namespace: n, autoDeploy: {from: [pr], branches: [main]}}]`, "requires from"},
		"ansible missing": {`defaults: {registries: [r/]}` + "\n" + `targets: [{id: a, project: o/r, type: ansible}]`, "ansible block"},
		"bad duration":    {`defaults: {lockDuration: soon}`, "invalid duration"},
		"bad type":        {`defaults: {registries: [r/]}` + "\n" + `targets: [{id: a, project: o/r, type: k8s}]`, "type must be"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestAllowsAutoDeploy(t *testing.T) {
	prev := Target{AutoDeploy: &AutoDeploy{From: []string{SourcePR}}}
	staging := Target{AutoDeploy: &AutoDeploy{From: []string{SourceBranch}, Branches: []string{"main"}}}
	none := Target{}
	cases := []struct {
		t              Target
		source, branch string
		want           bool
	}{
		{prev, SourcePR, "feature", true},
		{prev, SourceBranch, "main", false},
		{staging, SourceBranch, "main", true},
		{staging, SourceBranch, "feature", false},
		{staging, SourcePR, "main", false},
		{none, SourcePR, "", false},
	}
	for i, c := range cases {
		if got := c.t.AllowsAutoDeploy(c.source, c.branch); got != c.want {
			t.Errorf("case %d: got %v", i, got)
		}
	}
}

func TestParseDuration(t *testing.T) {
	if d, _ := ParseDuration("90d"); d != 90*24*time.Hour {
		t.Errorf("90d = %v", d)
	}
	if _, err := ParseDuration("xd"); err == nil {
		t.Error("xd accepted")
	}
}

func TestHolderKeepsOldConfigOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHolder(path)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("targets: [{id: BAD}]"), 0o600)
	h.Reload()
	if h.LastError() == nil || len(h.Get().Targets) != 4 {
		t.Fatal("broken config replaced the active one")
	}
	os.WriteFile(path, []byte(strings.Replace(valid, "app-test", "app-qa", 1)), 0o600)
	h.Reload()
	if h.LastError() != nil {
		t.Fatal(h.LastError())
	}
	if _, ok := h.Get().Target("app-qa"); !ok {
		t.Fatal("valid config not loaded")
	}
}
